//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type settledBillingContextRepo struct {
	UsageBillingRepository
	applyCtx context.Context
}

func (r *settledBillingContextRepo) Apply(ctx context.Context, _ *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	r.applyCtx = ctx
	balance := -1.0
	return &UsageBillingApplyResult{Applied: true, NewBalance: &balance}, nil
}

type settledBillingContextCache struct {
	BillingCache
	ctx       context.Context
	errAtCall error
}

func (c *settledBillingContextCache) InvalidateUserBalance(ctx context.Context, _ int64) error {
	c.ctx, c.errAtCall = ctx, ctx.Err()
	return nil
}

func TestUsageBillingCacheSyncGetsFreshContextAfterSettlement(t *testing.T) {
	type requestValueKey struct{}
	ctx := context.WithValue(context.Background(), requestValueKey{}, "request-pricing-context")
	repo := &settledBillingContextRepo{}
	cache := &settledBillingContextCache{}
	applied, err := applyUsageBilling(ctx, "settled-context", nil, &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: 1, TotalCost: 1},
		User: &User{ID: 1}, APIKey: &APIKey{ID: 2}, Account: &Account{ID: 3},
	}, &billingDeps{
		billingCacheService: &BillingCacheService{cache: cache, cfg: &config.Config{}},
		deferredService:     &DeferredService{},
	}, repo)
	require.NoError(t, err)
	require.True(t, applied)
	require.ErrorIs(t, repo.applyCtx.Err(), context.Canceled)
	require.NotNil(t, cache.ctx)
	require.NoError(t, cache.errAtCall, "post-commit cache work must not reuse the canceled or exhausted settlement context")
	require.Equal(t, "request-pricing-context", cache.ctx.Value(requestValueKey{}))
	deadline, ok := cache.ctx.Deadline()
	require.True(t, ok)
	require.Greater(t, time.Until(deadline), postUsageBillingTimeout-time.Second)
}
