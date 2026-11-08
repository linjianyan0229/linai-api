//go:build unit || integration

package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func runGroupDynamicRateContract(t *testing.T, db *sql.DB) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	exec := func(query string) { t.Helper(); _, err := tx.Exec(query); require.NoError(t, err) }
	exec(`CREATE TEMP TABLE groups(id BIGINT PRIMARY KEY, platform TEXT NOT NULL) ON COMMIT DROP;
CREATE TEMP TABLE api_keys(group_id BIGINT, key TEXT, deleted_at TIMESTAMPTZ) ON COMMIT DROP;
CREATE TEMP TABLE auth_cache_invalidation_outbox(cache_key TEXT) ON COMMIT DROP;`)
	migration, err := os.ReadFile("../../migrations/256_group_dynamic_rate.sql")
	require.NoError(t, err)
	exec(string(migration))
	exec(`INSERT INTO groups(id,platform) VALUES(1,'openai'),(2,'anthropic');
INSERT INTO api_keys VALUES(1,'dynamic-contract-key',NULL),(1,'deleted',NOW()),(2,'other',NULL);`)
	var enabled bool
	var factor float64
	require.NoError(t, tx.QueryRow(`SELECT dynamic_rate_enabled,dynamic_rate_factor FROM groups WHERE id=1`).Scan(&enabled, &factor))
	require.False(t, enabled)
	require.Equal(t, 2.0, factor)
	exec(`UPDATE groups SET dynamic_rate_enabled=TRUE,dynamic_rate_factor=2.5 WHERE id=1`)
	var count int
	var key string
	require.NoError(t, tx.QueryRow(`SELECT COUNT(*),MIN(cache_key) FROM auth_cache_invalidation_outbox`).Scan(&count, &key))
	require.Equal(t, 1, count)
	var expected string
	require.NoError(t, tx.QueryRow(`SELECT encode(sha256(convert_to('dynamic-contract-key','UTF8')),'hex')`).Scan(&expected))
	require.Equal(t, expected, key)
	exec(`UPDATE groups SET dynamic_rate_enabled=TRUE,dynamic_rate_factor=2.5 WHERE id=1`)
	require.NoError(t, tx.QueryRow(`SELECT COUNT(*) FROM auth_cache_invalidation_outbox`).Scan(&count))
	require.Equal(t, 1, count, "no-op saves do not produce invalidations")
	exec(`UPDATE groups SET dynamic_rate_enabled=FALSE WHERE id=1`)
	require.NoError(t, tx.QueryRow(`SELECT COUNT(*) FROM auth_cache_invalidation_outbox`).Scan(&count))
	require.Equal(t, 2, count)
	for _, query := range []string{`UPDATE groups SET dynamic_rate_enabled=TRUE WHERE id=2`, `UPDATE groups SET dynamic_rate_factor=-1 WHERE id=1`, `UPDATE groups SET dynamic_rate_factor=0 WHERE id=1`, `UPDATE groups SET dynamic_rate_factor=10000 WHERE id=1`} {
		exec(`SAVEPOINT invalid_config`)
		_, err := tx.Exec(query)
		require.Error(t, err)
		exec(`ROLLBACK TO SAVEPOINT invalid_config`)
	}
}
