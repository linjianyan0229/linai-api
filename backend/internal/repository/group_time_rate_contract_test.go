//go:build unit || integration

package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func runGroupTimeRateContract(t *testing.T, db *sql.DB) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	exec := func(query string) {
		t.Helper()
		_, err := tx.ExecContext(context.Background(), query)
		require.NoError(t, err)
	}
	exec(`CREATE TEMP TABLE groups(id BIGINT PRIMARY KEY,recharge_rate_tiers JSONB NOT NULL DEFAULT '[]') ON COMMIT DROP;
CREATE TEMP TABLE api_keys(group_id BIGINT,key TEXT,deleted_at TIMESTAMPTZ) ON COMMIT DROP;
CREATE TEMP TABLE auth_cache_invalidation_outbox(cache_key TEXT) ON COMMIT DROP;`)
	migration, err := os.ReadFile("../../migrations/253_group_time_rate_periods.sql")
	require.NoError(t, err)
	exec(string(migration))
	exec(`INSERT INTO groups(id) VALUES(1);
INSERT INTO api_keys VALUES(1,'time-rate-contract-key',NULL),(1,'deleted-key',NOW()),(2,'other-group',NULL);`)
	var periods string
	require.NoError(t, tx.QueryRowContext(context.Background(), `SELECT time_rate_periods::text FROM groups WHERE id=1`).Scan(&periods))
	require.JSONEq(t, `[]`, periods)
	exec(`UPDATE groups SET time_rate_periods='[{"start":"08:00","end":"20:00","rate_multiplier":1.2},{"start":"20:00","end":"08:00","rate_multiplier":0.6}]' WHERE id=1`)
	var count int
	require.NoError(t, tx.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM auth_cache_invalidation_outbox`).Scan(&count))
	require.Equal(t, 1, count, "only active keys bound to this group are invalidated")
	var correctHash bool
	require.NoError(t, tx.QueryRowContext(context.Background(), `SELECT cache_key=encode(sha256(convert_to('time-rate-contract-key','UTF8')),'hex') FROM auth_cache_invalidation_outbox`).Scan(&correctHash))
	require.True(t, correctHash)
	exec(`UPDATE groups SET time_rate_periods=time_rate_periods WHERE id=1`)
	require.NoError(t, tx.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM auth_cache_invalidation_outbox`).Scan(&count))
	require.Equal(t, 1, count, "unchanged rules do not enqueue invalidations")
	exec(`UPDATE groups SET recharge_rate_tiers='[{"min_amount":0,"rate_multiplier":1},{"min_amount":100,"rate_multiplier":0.75},{"min_amount":500,"rate_multiplier":0.5}]' WHERE id=1`)
	require.NoError(t, tx.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM auth_cache_invalidation_outbox`).Scan(&count))
	require.Equal(t, 2, count, "changing absolute-versus-stacked pricing invalidates snapshots")
	exec(`SAVEPOINT invalid_shape`)
	_, err = tx.ExecContext(context.Background(), `UPDATE groups SET time_rate_periods='[{}]' WHERE id=1`)
	require.Error(t, err)
	exec(`ROLLBACK TO SAVEPOINT invalid_shape`)
	exec(`UPDATE groups SET time_rate_periods='[]' WHERE id=1`)
	require.NoError(t, tx.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM auth_cache_invalidation_outbox`).Scan(&count))
	require.Equal(t, 3, count)
}
