package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/lib/pq"
)

type apiKeyCostIndexRelation struct {
	oid, parentOID int64
	schema, name   string
	kind           string
}

type apiKeyCostIndexState struct {
	oid, parentOID int64
	schema, name   string
	kind           string
	valid, ready   bool
	matches        bool
}

// The prefix plus an unsigned PostgreSQL OID is shorter than PostgreSQL's
// 63-byte identifier limit, even when a partition has a long or quoted name.
func apiKeyCostPartitionIndexName(oid int64) string {
	return usageLogsAPIKeyCostIndex + "_" + strconv.FormatInt(oid, 10)
}

func qualifiedMigrationIdentifier(schema, name string) string {
	return pq.QuoteIdentifier(schema) + "." + pq.QuoteIdentifier(name)
}

// migrateUsageLogsAPIKeyCostIndex runs on the migration's dedicated, non-tx
// advisory-lock connection. ONLY creates metadata for partitioned parents;
// every heap index is built CONCURRENTLY, then attached from the bottom up.
// Interrupted runs retain parent metadata and completed/attached child indexes.
func migrateUsageLogsAPIKeyCostIndex(ctx context.Context, db migrationConnection) error {
	var rootOID int64
	if err := db.QueryRowContext(ctx, `SELECT 'usage_logs'::regclass::oid::bigint`).Scan(&rootOID); err != nil {
		return fmt.Errorf("resolve usage_logs: %w", err)
	}
	relations, err := loadAPIKeyCostIndexRelations(ctx, db, rootOID)
	if err != nil {
		return err
	}
	if len(relations) == 0 {
		return errors.New("usage_logs relation tree is empty")
	}
	states := make(map[int64]*apiKeyCostIndexState, len(relations))
	for _, relation := range relations {
		if relation.kind != "r" && relation.kind != "p" {
			return fmt.Errorf("unsupported usage_logs partition %s of relation kind %q", qualifiedMigrationIdentifier(relation.schema, relation.name), relation.kind)
		}
		name := usageLogsAPIKeyCostIndex
		var state *apiKeyCostIndexState
		if relation.parentOID != 0 {
			name = apiKeyCostPartitionIndexName(relation.oid)
			parent := states[relation.parentOID]
			if parent == nil {
				return fmt.Errorf("missing parent index for relation %d", relation.oid)
			}
			// Attached indexes can have PostgreSQL-generated or operator-chosen names.
			state, err = findAttachedAPIKeyCostIndex(ctx, db, relation, parent.oid)
			if err != nil {
				return err
			}
		}
		if state == nil {
			state, err = findAPIKeyCostIndexByName(ctx, db, relation, name)
			if err != nil {
				return err
			}
		}
		state, err = ensureAPIKeyCostIndex(ctx, db, relation, name, state)
		if err != nil {
			return err
		}
		states[relation.oid] = state
		if relation.oid == rootOID && state.valid && state.ready {
			// PostgreSQL maintains the attached partition tree for a valid parent.
			return nil
		}
	}

	// Relations are parent-first. A subpartitioned index must be valid (all of
	// its own children attached) before it can be attached to its parent.
	for i := len(relations) - 1; i >= 0; i-- {
		relation := relations[i]
		if relation.parentOID == 0 {
			continue
		}
		state, err := readAPIKeyCostIndex(ctx, db, relation, states[relation.oid].oid)
		if err != nil {
			return err
		}
		if state == nil || !state.valid || !state.ready {
			return fmt.Errorf("partition index for %s is not valid and ready", qualifiedMigrationIdentifier(relation.schema, relation.name))
		}
		parent := states[relation.parentOID]
		if state.parentOID == parent.oid {
			continue
		}
		if state.parentOID != 0 {
			return fmt.Errorf("index %s is already attached to another parent", qualifiedMigrationIdentifier(state.schema, state.name))
		}
		statement := "ALTER INDEX " + qualifiedMigrationIdentifier(parent.schema, parent.name) +
			" ATTACH PARTITION " + qualifiedMigrationIdentifier(state.schema, state.name)
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("attach API key cost index for %s: %w", qualifiedMigrationIdentifier(relation.schema, relation.name), err)
		}
	}
	root, err := readAPIKeyCostIndex(ctx, db, relations[0], states[rootOID].oid)
	if err != nil {
		return err
	}
	if root == nil || !root.valid || !root.ready {
		return errors.New("usage_logs API key cost index tree is not valid; retry after concurrent partition maintenance finishes")
	}
	return nil
}

func loadAPIKeyCostIndexRelations(ctx context.Context, db migrationConnection, rootOID int64) ([]apiKeyCostIndexRelation, error) {
	rows, err := db.QueryContext(ctx, `
WITH RECURSIVE relation_tree AS (
    SELECT c.oid, 0::oid AS parent_oid, 0 AS depth
    FROM pg_class c WHERE c.oid = $1::oid
    UNION ALL
    SELECT child.oid, parent.oid, parent.depth + 1
    FROM relation_tree parent
    JOIN pg_inherits inheritance ON inheritance.inhparent = parent.oid
    JOIN pg_class child ON child.oid = inheritance.inhrelid
    JOIN pg_class parent_class ON parent_class.oid = parent.oid AND parent_class.relkind = 'p'
)
SELECT tree.oid::bigint, tree.parent_oid::bigint, ns.nspname, c.relname, c.relkind::text
FROM relation_tree tree
JOIN pg_class c ON c.oid = tree.oid
JOIN pg_namespace ns ON ns.oid = c.relnamespace
ORDER BY tree.depth, tree.oid`, rootOID)
	if err != nil {
		return nil, fmt.Errorf("read usage_logs partition tree: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var relations []apiKeyCostIndexRelation
	for rows.Next() {
		var relation apiKeyCostIndexRelation
		if err := rows.Scan(&relation.oid, &relation.parentOID, &relation.schema, &relation.name, &relation.kind); err != nil {
			return nil, err
		}
		relations = append(relations, relation)
	}
	return relations, rows.Err()
}

func findAPIKeyCostIndexByName(ctx context.Context, db migrationConnection, relation apiKeyCostIndexRelation, name string) (*apiKeyCostIndexState, error) {
	var oid int64
	err := db.QueryRowContext(ctx, `SELECT c.oid::bigint FROM pg_class c
JOIN pg_namespace ns ON ns.oid = c.relnamespace
WHERE ns.nspname = $1 AND c.relname = $2`, relation.schema, name).Scan(&oid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	state, err := readAPIKeyCostIndex(ctx, db, relation, oid)
	if err == nil && state == nil {
		err = fmt.Errorf("relation %s exists but is not the expected index", qualifiedMigrationIdentifier(relation.schema, name))
	}
	return state, err
}

func findAttachedAPIKeyCostIndex(ctx context.Context, db migrationConnection, relation apiKeyCostIndexRelation, parentIndexOID int64) (*apiKeyCostIndexState, error) {
	var oid int64
	err := db.QueryRowContext(ctx, `SELECT i.indexrelid::bigint FROM pg_index i
JOIN pg_inherits inheritance ON inheritance.inhrelid = i.indexrelid
WHERE i.indrelid = $1::oid AND inheritance.inhparent = $2::oid`, relation.oid, parentIndexOID).Scan(&oid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return readAPIKeyCostIndex(ctx, db, relation, oid)
}

func readAPIKeyCostIndex(ctx context.Context, db migrationConnection, relation apiKeyCostIndexRelation, oid int64) (*apiKeyCostIndexState, error) {
	state := &apiKeyCostIndexState{}
	err := db.QueryRowContext(ctx, `SELECT c.oid::bigint, ns.nspname, c.relname, c.relkind::text,
    i.indisvalid, i.indisready,
    i.indrelid = $2::oid AND am.amname = 'btree'
      AND NOT i.indisunique AND NOT i.indisprimary AND NOT i.indisexclusion
      AND i.indnkeyatts = 2 AND i.indnatts = 3
      AND i.indexprs IS NULL AND i.indpred IS NULL
      AND pg_get_indexdef(c.oid, 1, true) = 'api_key_id'
      AND pg_get_indexdef(c.oid, 2, true) = 'created_at'
      AND pg_get_indexdef(c.oid, 3, true) = 'actual_cost'
      AND i.indoption[0] = 0 AND i.indoption[1] = 0,
    COALESCE((SELECT inhparent::bigint FROM pg_inherits WHERE inhrelid = c.oid), 0)
FROM pg_class c
JOIN pg_namespace ns ON ns.oid = c.relnamespace
JOIN pg_index i ON i.indexrelid = c.oid
JOIN pg_am am ON am.oid = c.relam
WHERE c.oid = $1::oid`, oid, relation.oid).Scan(
		&state.oid, &state.schema, &state.name, &state.kind, &state.valid, &state.ready, &state.matches, &state.parentOID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return state, err
}

func ensureAPIKeyCostIndex(ctx context.Context, db migrationConnection, relation apiKeyCostIndexRelation, name string, state *apiKeyCostIndexState) (*apiKeyCostIndexState, error) {
	if state != nil {
		if !state.matches || (relation.kind == "p") != (state.kind == "I") {
			return nil, fmt.Errorf("index %s has an unexpected definition; refusing to replace it", qualifiedMigrationIdentifier(state.schema, state.name))
		}
		if relation.kind == "p" || (state.valid && state.ready) {
			return state, nil
		}
		if state.parentOID != 0 {
			return nil, fmt.Errorf("invalid index %s is attached; refusing to drop it", qualifiedMigrationIdentifier(state.schema, state.name))
		}
		// Only an unattached heap index left by interrupted CIC is removed.
		if _, err := db.ExecContext(ctx, "DROP INDEX CONCURRENTLY IF EXISTS "+qualifiedMigrationIdentifier(state.schema, state.name)); err != nil {
			return nil, fmt.Errorf("drop interrupted API key cost index: %w", err)
		}
	}
	concurrent, only := " CONCURRENTLY", ""
	if relation.kind == "p" {
		concurrent, only = "", "ONLY "
	}
	statement := "CREATE INDEX" + concurrent + " IF NOT EXISTS " + pq.QuoteIdentifier(name) +
		" ON " + only + qualifiedMigrationIdentifier(relation.schema, relation.name) +
		" (api_key_id, created_at) INCLUDE (actual_cost)"
	if _, err := db.ExecContext(ctx, statement); err != nil {
		return nil, fmt.Errorf("create API key cost index for %s: %w", qualifiedMigrationIdentifier(relation.schema, relation.name), err)
	}
	created, err := findAPIKeyCostIndexByName(ctx, db, relation, name)
	if err != nil {
		return nil, err
	}
	if created == nil || !created.matches || (relation.kind == "p") != (created.kind == "I") {
		return nil, fmt.Errorf("API key cost index for %s was not created with the expected definition", qualifiedMigrationIdentifier(relation.schema, relation.name))
	}
	if relation.kind == "r" && (!created.valid || !created.ready) {
		return nil, fmt.Errorf("API key cost index for %s did not become valid", qualifiedMigrationIdentifier(relation.schema, relation.name))
	}
	return created, nil
}
