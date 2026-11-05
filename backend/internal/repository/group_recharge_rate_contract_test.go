package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// Uses an isolated connection's temporary tables; never touches live accounting.
func runGroupRechargeRateContract(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := tx.ExecContext(ctx, query, args...)
		require.NoError(t, err)
	}
	exec(`CREATE TEMP TABLE groups(id BIGINT PRIMARY KEY,subscription_type TEXT DEFAULT 'standard',deleted_at TIMESTAMPTZ) ON COMMIT DROP;
CREATE TEMP TABLE user_group_rate_multipliers(user_id BIGINT,group_id BIGINT,rate_multiplier NUMERIC,PRIMARY KEY(user_id,group_id)) ON COMMIT DROP;
CREATE TEMP TABLE payment_orders(id BIGINT PRIMARY KEY,user_id BIGINT,amount NUMERIC(20,2),bonus_amount NUMERIC(20,2) DEFAULT 0,pay_amount NUMERIC(20,2) DEFAULT 0,refund_amount NUMERIC(20,2) DEFAULT 0,order_type TEXT DEFAULT 'balance',status TEXT DEFAULT 'PENDING',completed_at TIMESTAMPTZ) ON COMMIT DROP;`)
	migration, err := os.ReadFile("../../migrations/252_group_recharge_rate_tiers.sql")
	require.NoError(t, err)
	exec(string(migration))
	exec(`INSERT INTO groups(id) VALUES(1),(2),(3),(4);
UPDATE groups SET recharge_rate_tiers='[{"min_amount":0,"rate_multiplier":1},{"min_amount":100,"rate_multiplier":0.75},{"min_amount":500,"rate_multiplier":0.5}]' WHERE id IN (1,3,4);
UPDATE groups SET subscription_type='subscription' WHERE id=3;
UPDATE groups SET deleted_at=NOW() WHERE id=4;`)
	repo := &userGroupRateRepository{sql: tx}
	check := func(principal, rate float64) {
		t.Helper()
		total, err := repo.GetRechargePrincipal(ctx, 10)
		require.NoError(t, err)
		require.InDelta(t, principal, total, 1e-8)
		got, err := repo.GetByUserAndGroup(ctx, 10, 1)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.InDelta(t, rate, *got, 1e-8)
		rates, err := repo.GetEffectiveByUserID(ctx, 10)
		require.NoError(t, err)
		require.Equal(t, map[int64]float64{1: rate}, rates)
	}
	check(0, 1)
	// Bonus and CNY gateway amount are not USD principal. Pending, subscription,
	// failed and unfulfilled orders cannot grant a level.
	exec(`INSERT INTO payment_orders(id,user_id,amount,bonus_amount,pay_amount,status,completed_at,order_type) VALUES
(1,10,120,20,700,'COMPLETED',NOW(),'balance'),
(2,10,100000,0,100000,'PENDING',NULL,'balance'),
(3,10,100000,0,100000,'COMPLETED',NOW(),'subscription'),
(4,10,100000,0,100000,'FAILED',NULL,'balance'),
(5,10,100000,0,100000,'RECHARGING',NULL,'balance'),
(6,11,100000,0,100000,'COMPLETED',NOW(),'balance');`)
	check(100, 0.75)
	exec(`INSERT INTO payment_orders(id,user_id,amount,status,completed_at) VALUES(7,10,400,'COMPLETED',NOW())`)
	check(500, 0.5)
	exec(`UPDATE payment_orders SET status='COMPLETED',completed_at=NOW() WHERE id=7`)
	check(500, 0.5) // A repeated success notification cannot add principal twice.
	for _, status := range []string{"REFUND_REQUESTED", "REFUNDING", "REFUND_PENDING", "REFUND_FAILED"} {
		exec(`UPDATE payment_orders SET status=$1,refund_amount=60 WHERE id=1`, status)
		check(500, 0.5)
	}
	exec(`UPDATE payment_orders SET status='PARTIALLY_REFUNDED',refund_amount=60 WHERE id=1`)
	check(450, 0.75)
	exec(`UPDATE payment_orders SET status='REFUNDED',refund_amount=400 WHERE id=7`)
	check(50, 1)
	exec(`INSERT INTO user_group_rate_multipliers(user_id,group_id,rate_multiplier) VALUES(10,1,0.6)`)
	check(50, 0.6) // Manual agreement has priority over recharge levels.
	manual, err := repo.GetByUserID(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, map[int64]float64{1: 0.6}, manual)
	exec(`DELETE FROM user_group_rate_multipliers WHERE user_id=10`)
	manual, err = repo.GetByUserID(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, manual, "derived rates must never become admin overrides")
	for _, id := range []int64{2, 3, 4, 999} {
		rate, err := repo.GetByUserAndGroup(ctx, 10, id)
		require.NoError(t, err)
		require.Nil(t, rate, "disabled, subscription, deleted, and missing groups use no earned override")
	}
	exec(`UPDATE groups SET recharge_rate_tiers='[]' WHERE id=1`)
	rate, err := repo.GetByUserAndGroup(ctx, 10, 1)
	require.NoError(t, err)
	require.Nil(t, rate)
}
