package repository

import (
	"context"
	"database/sql"
	"testing"
	"testing/fstest"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestApplyMigrationsFS_NonTransactionalMigration_APIKeyCostIndexDropsInvalidIndexBeforeRetry(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs(usageLogsAPIKeyCostIndexMigration).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT 'usage_logs'::regclass::oid::bigint").
		WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(100))
	mock.ExpectQuery("WITH RECURSIVE relation_tree").WithArgs(int64(100)).
		WillReturnRows(sqlmock.NewRows([]string{"oid", "parent_oid", "schema", "name", "kind"}).AddRow(100, 0, "public", "usage_logs", "r"))
	expectIndex := func(valid bool) {
		mock.ExpectQuery("SELECT c.oid::bigint FROM pg_class c").WithArgs("public", usageLogsAPIKeyCostIndex).
			WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(200))
		mock.ExpectQuery("SELECT c.oid::bigint, ns.nspname, c.relname").WithArgs(int64(200), int64(100)).
			WillReturnRows(sqlmock.NewRows([]string{"oid", "schema", "name", "kind", "valid", "ready", "matches", "parent_oid"}).
				AddRow(200, "public", usageLogsAPIKeyCostIndex, "i", valid, valid, true, 0))
	}
	expectIndex(false)
	mock.ExpectExec(`DROP INDEX CONCURRENTLY IF EXISTS "public"\."idx_usage_logs_api_key_created_cost"`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE INDEX CONCURRENTLY IF NOT EXISTS "idx_usage_logs_api_key_created_cost" ON "public"\."usage_logs"`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	expectIndex(true)
	mock.ExpectExec("INSERT INTO schema_migrations \\(filename, checksum\\) VALUES \\(\\$1, \\$2\\)").
		WithArgs(usageLogsAPIKeyCostIndexMigration, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	fsys := fstest.MapFS{
		usageLogsAPIKeyCostIndexMigration: &fstest.MapFile{
			Data: []byte(`
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_api_key_created_cost
    ON usage_logs (api_key_id, created_at)
    INCLUDE (actual_cost);
`),
		},
	}

	err = applyMigrationsFS(context.Background(), db, fsys)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAPIKeyCostIndexKeepsInvalidPartitionedParentOnRetry(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	parent := &apiKeyCostIndexState{oid: 200, schema: "quoted schema", name: usageLogsAPIKeyCostIndex, kind: "I", matches: true}
	got, err := ensureAPIKeyCostIndex(context.Background(), db, apiKeyCostIndexRelation{oid: 100, kind: "p"}, parent.name, parent)
	require.NoError(t, err)
	require.Same(t, parent, got)
	require.NoError(t, mock.ExpectationsWereMet(), "retry must not drop partitioned parent metadata")
}

func TestAPIKeyCostIndexRefusesToDropAttachedOrUnexpectedIndex(t *testing.T) {
	for _, state := range []*apiKeyCostIndexState{
		{oid: 200, kind: "i", matches: true, parentOID: 300},
		{oid: 200, kind: "i", matches: false},
	} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		_, err = ensureAPIKeyCostIndex(context.Background(), db, apiKeyCostIndexRelation{oid: 100, kind: "r"}, "index", state)
		require.Error(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	}
}

func TestAPIKeyCostIndexIdentifiersAreQuotedAndShort(t *testing.T) {
	require.Equal(t, `"a""b"."x.y"`, qualifiedMigrationIdentifier(`a"b`, "x.y"))
	name := apiKeyCostPartitionIndexName(4294967295)
	require.LessOrEqual(t, len(name), 63)
	require.NotEqual(t, name, apiKeyCostPartitionIndexName(4294967294))
}
