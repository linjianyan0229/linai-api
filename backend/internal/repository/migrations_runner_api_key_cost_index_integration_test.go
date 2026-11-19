//go:build integration

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func newAPIKeyCostIndexMigrationDB(t *testing.T, partitioned bool) (*sql.DB, string) {
	t.Helper()
	schema := fmt.Sprintf("api key \"cost %d", time.Now().UnixNano())
	quoted := pq.QuoteIdentifier(schema)
	_, err := integrationDB.ExecContext(context.Background(), "CREATE SCHEMA "+quoted)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+quoted+" CASCADE")
	})
	parsed, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("search_path", quoted+",pg_catalog")
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	db.SetMaxOpenConns(5)
	t.Cleanup(func() { _ = db.Close() })
	// The legacy bootstrap discovers public.atlas_schema_revisions, while this
	// fixture deliberately excludes public from search_path. Provide its isolated
	// counterpart so bootstrap never reads or changes the shared harness baseline.
	_, err = db.ExecContext(context.Background(), atlasSchemaRevisionsTableDDL)
	require.NoError(t, err)
	ddl := `CREATE TABLE usage_logs (api_key_id bigint NOT NULL, created_at timestamptz NOT NULL, actual_cost numeric NOT NULL)`
	if partitioned {
		ddl += ` PARTITION BY RANGE (created_at)`
	}
	_, err = db.ExecContext(context.Background(), ddl)
	require.NoError(t, err)
	if partitioned {
		_, err = db.ExecContext(context.Background(), `
CREATE TABLE "branch""mid" PARTITION OF usage_logs
    FOR VALUES FROM ('2026-01-01') TO ('2027-01-01') PARTITION BY HASH (api_key_id);
CREATE TABLE `+pq.QuoteIdentifier(strings.Repeat("long_", 11)+`leaf"a`)+` PARTITION OF "branch""mid"
    FOR VALUES WITH (MODULUS 2, REMAINDER 0);
CREATE TABLE "leaf b" PARTITION OF "branch""mid"
    FOR VALUES WITH (MODULUS 2, REMAINDER 1);
CREATE TABLE usage_logs_default PARTITION OF usage_logs DEFAULT;`)
		require.NoError(t, err)
	}
	return db, schema
}

func apiKeyCostIndexMigrationFS(t *testing.T) fstest.MapFS {
	t.Helper()
	content, err := migrations.FS.ReadFile(usageLogsAPIKeyCostIndexMigration)
	require.NoError(t, err)
	return fstest.MapFS{usageLogsAPIKeyCostIndexMigration: &fstest.MapFile{Data: content}}
}

func assertAPIKeyCostIndexTree(t *testing.T, db *sql.DB) map[int64]*apiKeyCostIndexState {
	t.Helper()
	ctx := context.Background()
	var rootOID int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT 'usage_logs'::regclass::oid::bigint`).Scan(&rootOID))
	relations, err := loadAPIKeyCostIndexRelations(ctx, db, rootOID)
	require.NoError(t, err)
	states := make(map[int64]*apiKeyCostIndexState)
	for _, relation := range relations {
		var state *apiKeyCostIndexState
		if relation.parentOID == 0 {
			state, err = findAPIKeyCostIndexByName(ctx, db, relation, usageLogsAPIKeyCostIndex)
		} else {
			state, err = findAttachedAPIKeyCostIndex(ctx, db, relation, states[relation.parentOID].oid)
		}
		require.NoError(t, err)
		require.NotNil(t, state, "missing index for %s", relation.name)
		require.True(t, state.matches, "wrong definition for %s", relation.name)
		require.True(t, state.valid, "invalid index for %s", relation.name)
		require.True(t, state.ready, "unready index for %s", relation.name)
		states[relation.oid] = state
	}
	return states
}

func TestAPIKeyCostIndexMigrationRegularAndMultiLevelPartitioned(t *testing.T) {
	for _, partitioned := range []bool{false, true} {
		t.Run(fmt.Sprintf("partitioned=%t", partitioned), func(t *testing.T) {
			db, _ := newAPIKeyCostIndexMigrationDB(t, partitioned)
			fsys := apiKeyCostIndexMigrationFS(t)
			require.NoError(t, applyMigrationsFS(context.Background(), db, fsys))
			states := assertAPIKeyCostIndexTree(t, db)
			if partitioned {
				require.Len(t, states, 5, "root, subpartitioned branch and three heap leaves")
			} else {
				require.Len(t, states, 1)
			}
			// Both checksum replay and direct retry retain all completed indexes.
			require.NoError(t, applyMigrationsFS(context.Background(), db, fsys))
			require.NoError(t, migrateUsageLogsAPIKeyCostIndex(context.Background(), db))
			require.Equal(t, states, assertAPIKeyCostIndexTree(t, db))
			var applied int
			require.NoError(t, db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE filename = $1`, usageLogsAPIKeyCostIndexMigration).Scan(&applied))
			require.Equal(t, 1, applied)
		})
	}
}

type apiKeyCostIndexInterruptedConnection struct {
	migrationConnection
	statements  []string
	interrupted bool
}

func (c *apiKeyCostIndexInterruptedConnection) ExecContext(ctx context.Context, statement string, args ...any) (sql.Result, error) {
	c.statements = append(c.statements, statement)
	result, err := c.migrationConnection.ExecContext(ctx, statement, args...)
	if err == nil && !c.interrupted && strings.HasPrefix(statement, "ALTER INDEX ") {
		c.interrupted = true
		return nil, errors.New("simulated disconnect after successful partition attachment")
	}
	return result, err
}

func TestAPIKeyCostIndexMigrationResumesAttachedSubtreeWithoutDroppingParents(t *testing.T) {
	db, _ := newAPIKeyCostIndexMigrationDB(t, true)
	connection := &apiKeyCostIndexInterruptedConnection{migrationConnection: db}
	err := migrateUsageLogsAPIKeyCostIndex(context.Background(), connection)
	require.ErrorContains(t, err, "simulated disconnect")
	require.True(t, connection.interrupted)
	var rootOID, parentIndexOID, attachedLeafOID int64
	require.NoError(t, db.QueryRow(`SELECT 'usage_logs'::regclass::oid::bigint`).Scan(&rootOID))
	relations, err := loadAPIKeyCostIndexRelations(context.Background(), db, rootOID)
	require.NoError(t, err)
	rootState, err := findAPIKeyCostIndexByName(context.Background(), db, relations[0], usageLogsAPIKeyCostIndex)
	require.NoError(t, err)
	require.False(t, rootState.valid)
	parentIndexOID = rootState.oid
	// Reuse an attached index even if it has an arbitrary, quoted name.
	require.NoError(t, db.QueryRow(`SELECT i.indexrelid::bigint FROM pg_index i
JOIN pg_inherits inh ON inh.inhrelid = i.indexrelid
JOIN pg_class table_class ON table_class.oid = i.indrelid
JOIN pg_namespace ns ON ns.oid = table_class.relnamespace
WHERE ns.nspname = current_schema() AND table_class.relkind = 'r' LIMIT 1`).Scan(&attachedLeafOID))
	var attachedName string
	require.NoError(t, db.QueryRow(`SELECT relname FROM pg_class WHERE oid = $1::oid`, attachedLeafOID).Scan(&attachedName))
	_, err = db.Exec(`ALTER INDEX ` + pq.QuoteIdentifier(attachedName) + ` RENAME TO "already attached""leaf"`)
	require.NoError(t, err)
	require.NoError(t, applyMigrationsFS(context.Background(), db, apiKeyCostIndexMigrationFS(t)))
	states := assertAPIKeyCostIndexTree(t, db)
	require.Equal(t, parentIndexOID, states[rootOID].oid, "invalid parent metadata must survive retry")
	found := false
	for _, state := range states {
		if state.oid == attachedLeafOID {
			found = true
			require.Equal(t, `already attached"leaf`, state.name)
		}
	}
	require.True(t, found, "retry must reuse the previously attached leaf")
	for _, statement := range connection.statements {
		if strings.HasPrefix(statement, "CREATE INDEX ") && !strings.Contains(statement, " CONCURRENTLY ") {
			require.Contains(t, statement, " ON ONLY ", "parent creation must never synchronously build heap indexes")
		}
		require.NotContains(t, statement, "DROP INDEX", "an interrupted attachment never requires deleting parent metadata")
	}
}

// An uncommitted writer makes CIC publish its invalid catalog entry and wait.
// Cancel at that boundary to create the same recoverable artifact as a crashed migration.
func leaveInvalidAPIKeyCostIndex(t *testing.T, db *sql.DB, table, index string) int64 {
	t.Helper()
	writer, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer func() { _ = writer.Rollback() }()
	_, err = writer.Exec(`INSERT INTO ` + pq.QuoteIdentifier(table) + ` (api_key_id, created_at, actual_cost) VALUES (1, '2050-01-01', 0.1)`)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, buildErr := db.ExecContext(ctx, `CREATE INDEX CONCURRENTLY `+pq.QuoteIdentifier(index)+` ON `+pq.QuoteIdentifier(table)+` (api_key_id, created_at) INCLUDE (actual_cost)`)
		done <- buildErr
	}()
	var oid int64
	require.Eventually(t, func() bool {
		return db.QueryRow(`SELECT c.oid::bigint FROM pg_class c JOIN pg_index i ON i.indexrelid = c.oid
JOIN pg_namespace ns ON ns.oid = c.relnamespace
WHERE ns.nspname = current_schema() AND c.relname = $1 AND NOT i.indisvalid`, index).Scan(&oid) == nil
	}, 10*time.Second, 10*time.Millisecond, "CIC must publish an invalid index before cancellation")
	cancel()
	select {
	case buildErr := <-done:
		require.Error(t, buildErr)
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled concurrent index build did not stop")
	}
	return oid
}

func TestAPIKeyCostIndexMigrationRepairsInterruptedHeapBuild(t *testing.T) {
	for _, partitioned := range []bool{false, true} {
		t.Run(fmt.Sprintf("partitioned=%t", partitioned), func(t *testing.T) {
			db, _ := newAPIKeyCostIndexMigrationDB(t, partitioned)
			table, index := "usage_logs", usageLogsAPIKeyCostIndex
			if partitioned {
				table = "usage_logs_default"
				var leafOID int64
				require.NoError(t, db.QueryRow(`SELECT 'usage_logs_default'::regclass::oid::bigint`).Scan(&leafOID))
				index = apiKeyCostPartitionIndexName(leafOID)
			}
			invalidOID := leaveInvalidAPIKeyCostIndex(t, db, table, index)
			require.NoError(t, applyMigrationsFS(context.Background(), db, apiKeyCostIndexMigrationFS(t)))
			states := assertAPIKeyCostIndexTree(t, db)
			for _, state := range states {
				require.NotEqual(t, invalidOID, state.oid, "only the interrupted heap index should be replaced")
			}
		})
	}
}
