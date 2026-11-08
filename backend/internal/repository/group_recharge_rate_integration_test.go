//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupRechargeRateIntegration(t *testing.T) {
	runGroupRechargeRateContract(t, integrationDB)
}

func TestGroupRechargeRatePersistence(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	repo := newGroupRepositoryWithSQL(tx.Client(), tx)
	tiers := []service.GroupRechargeRateTier{{MinAmount: 0, RateMultiplier: 1}, {MinAmount: 100, RateMultiplier: 0.75}, {MinAmount: 500, RateMultiplier: 0.5}}
	group := &service.Group{DynamicRateEnabled: true, DynamicRateFactor: 2.5, Name: "recharge-roundtrip", Platform: service.PlatformOpenAI, RateMultiplier: 1, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RechargeRateTiers: tiers, TimeRatePeriods: []service.GroupTimeRatePeriod{{Kind: "peak", Start: "08:00", End: "12:00", RateMultiplier: 1.2}, {Kind: "valley", Start: "12:00", End: "18:00", RateMultiplier: 0.6}, {Kind: "peak", Start: "18:00", End: "22:00", RateMultiplier: 1.1}, {Kind: "valley", Start: "22:00", End: "08:00", RateMultiplier: 0.5}}}
	require.NoError(t, repo.Create(ctx, group))
	got, err := repo.GetByID(ctx, group.ID)
	require.NoError(t, err)
	require.True(t, got.DynamicRateEnabled)
	require.Equal(t, 2.5, got.DynamicRateFactor)
	require.Equal(t, tiers, got.RechargeRateTiers)
	require.Equal(t, group.TimeRatePeriods, got.TimeRatePeriods)
	got.RechargeRateTiers[1].MinAmount = 250
	require.NoError(t, repo.Update(ctx, got))
	got, err = repo.GetByIDLite(ctx, group.ID)
	require.NoError(t, err)
	require.Equal(t, float64(250), got.RechargeRateTiers[1].MinAmount)
	got.DynamicRateEnabled = false
	got.DynamicRateFactor = 3
	got.TimeRatePeriods = nil
	got.RechargeRateTiers = nil
	require.NoError(t, repo.Update(ctx, got))
	got, err = repo.GetByID(ctx, group.ID)
	require.NoError(t, err)
	require.Empty(t, got.RechargeRateTiers)
	require.False(t, got.DynamicRateEnabled)
	require.Equal(t, 3.0, got.DynamicRateFactor)
	require.Empty(t, got.TimeRatePeriods)
}
