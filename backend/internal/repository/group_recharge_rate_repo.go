package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.UserGroupRechargeRateRepository = (*userGroupRateRepository)(nil)

// amount and bonus_amount are immutable USD credit snapshots. Gateway pay_amount
// may be CNY and include fees, so it cannot be used for USD recharge levels.
// Refund amounts are in credited USD; partial refunds remove principal and bonus
// proportionally, matching the payment service's gateway-refund calculation.
// Requested/pending/failed refunds do not remove principal until actually settled.
const rechargePrincipalSQL = `
SELECT COALESCE(SUM(ROUND(
    GREATEST(amount - bonus_amount, 0) *
    CASE WHEN status IN ('REFUNDED', 'PARTIALLY_REFUNDED')
         THEN GREATEST(0, 1 - refund_amount / NULLIF(amount, 0))
         ELSE 1 END, 2)), 0)
FROM payment_orders
WHERE user_id = $1 AND order_type = 'balance' AND completed_at IS NOT NULL
  AND amount > 0 AND status IN (
    'COMPLETED', 'REFUND_REQUESTED', 'REFUNDING', 'REFUND_PENDING',
    'REFUND_FAILED', 'REFUNDED', 'PARTIALLY_REFUNDED')`

const rechargeRateJoinSQL = `
LEFT JOIN LATERAL (
    SELECT (tier->>'rate_multiplier')::numeric AS rate
    FROM jsonb_array_elements(g.recharge_rate_tiers) AS tier
    WHERE g.subscription_type = 'standard'
      AND (tier->>'min_amount')::numeric <= (` + rechargePrincipalSQL + `)
    ORDER BY (tier->>'min_amount')::numeric DESC
    LIMIT 1
) recharge ON TRUE`

func (r *userGroupRateRepository) GetRechargePrincipal(ctx context.Context, userID int64) (float64, error) {
	var total float64
	err := scanSingleRow(ctx, r.sql, rechargePrincipalSQL, []any{userID}, &total)
	return total, err
}

// GetEffectiveByUserID is for customer pricing displays only. Admin CRUD uses
// GetByUserID, which returns exclusively the manually assigned overrides.
func (r *userGroupRateRepository) GetEffectiveByUserID(ctx context.Context, userID int64) (map[int64]float64, error) {
	rows, err := r.sql.QueryContext(ctx, `
SELECT g.id, COALESCE(ugr.rate_multiplier, recharge.rate)
FROM groups g
LEFT JOIN user_group_rate_multipliers ugr ON ugr.group_id = g.id AND ugr.user_id = $1
`+rechargeRateJoinSQL+`
WHERE g.deleted_at IS NULL
  AND COALESCE(ugr.rate_multiplier, recharge.rate) IS NOT NULL`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	rates := make(map[int64]float64)
	for rows.Next() {
		var id int64
		var rate float64
		if err := rows.Scan(&id, &rate); err != nil {
			return nil, err
		}
		rates[id] = rate
	}
	return rates, rows.Err()
}

func (r *userGroupRateRepository) getEffectiveByUserAndGroup(ctx context.Context, userID, groupID int64) (*float64, error) {
	var rate sql.NullFloat64
	err := scanSingleRow(ctx, r.sql, `
SELECT COALESCE(ugr.rate_multiplier, recharge.rate)
FROM groups g
LEFT JOIN user_group_rate_multipliers ugr ON ugr.group_id = g.id AND ugr.user_id = $1
`+rechargeRateJoinSQL+`
WHERE g.id = $2 AND g.deleted_at IS NULL`, []any{userID, groupID}, &rate)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !rate.Valid {
		return nil, nil
	}
	return &rate.Float64, nil
}
