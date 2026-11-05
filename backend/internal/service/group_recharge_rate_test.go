package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testRechargeTiers() []GroupRechargeRateTier {
	return []GroupRechargeRateTier{
		{MinAmount: 0, RateMultiplier: 1},
		{MinAmount: 100, RateMultiplier: 0.75},
		{MinAmount: 500, RateMultiplier: 0.5},
	}
}

func TestGroupRechargeRateValidation(t *testing.T) {
	require.NoError(t, ValidateGroupRechargeRateTiers(nil, SubscriptionTypeStandard))
	require.NoError(t, ValidateGroupRechargeRateTiers(testRechargeTiers(), SubscriptionTypeStandard))
	require.Error(t, ValidateGroupRechargeRateTiers(testRechargeTiers(), SubscriptionTypeSubscription))
	for name, mutate := range map[string]func([]GroupRechargeRateTier) []GroupRechargeRateTier{
		"two levels":       func(v []GroupRechargeRateTier) []GroupRechargeRateTier { return v[:2] },
		"nonzero first":    func(v []GroupRechargeRateTier) []GroupRechargeRateTier { v[0].MinAmount = 1; return v },
		"same threshold":   func(v []GroupRechargeRateTier) []GroupRechargeRateTier { v[2].MinAmount = 100; return v },
		"fractional cent":  func(v []GroupRechargeRateTier) []GroupRechargeRateTier { v[1].MinAmount = 100.001; return v },
		"negative":         func(v []GroupRechargeRateTier) []GroupRechargeRateTier { v[1].MinAmount = -1; return v },
		"overflow":         func(v []GroupRechargeRateTier) []GroupRechargeRateTier { v[2].MinAmount = 1e13; return v },
		"infinite":         func(v []GroupRechargeRateTier) []GroupRechargeRateTier { v[2].MinAmount = math.Inf(1); return v },
		"nan":              func(v []GroupRechargeRateTier) []GroupRechargeRateTier { v[2].RateMultiplier = math.NaN(); return v },
		"increasing rate":  func(v []GroupRechargeRateTier) []GroupRechargeRateTier { v[2].RateMultiplier = 0.9; return v },
		"rate below limit": func(v []GroupRechargeRateTier) []GroupRechargeRateTier { v[2].RateMultiplier = 0.49; return v },
		"rate above limit": func(v []GroupRechargeRateTier) []GroupRechargeRateTier { v[0].RateMultiplier = 1.1; return v },
		"rate precision":   func(v []GroupRechargeRateTier) []GroupRechargeRateTier { v[1].RateMultiplier = 0.75001; return v },
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, ValidateGroupRechargeRateTiers(mutate(testRechargeTiers()), SubscriptionTypeStandard))
		})
	}
}

func TestGroupRechargeLevelBoundaries(t *testing.T) {
	for _, tc := range []struct {
		principal float64
		level     int
	}{{0, 1}, {99.99, 1}, {100, 2}, {499.99, 2}, {500, 3}, {1e8, 3}} {
		require.Equal(t, tc.level, GroupRechargeLevel(testRechargeTiers(), tc.principal))
	}
	require.Zero(t, GroupRechargeLevel(nil, 100))
}

type rechargeRateTestRepo struct {
	userGroupRateResolverRepoStub
}

func (*rechargeRateTestRepo) GetEffectiveByUserID(context.Context, int64) (map[int64]float64, error) {
	return map[int64]float64{8: 0.5}, nil
}
func (*rechargeRateTestRepo) GetRechargePrincipal(context.Context, int64) (float64, error) {
	return 500, nil
}

func TestGroupRechargeRateResolverAndDisplay(t *testing.T) {
	rate := 0.5
	repo := &rechargeRateTestRepo{userGroupRateResolverRepoStub: userGroupRateResolverRepoStub{rate: &rate}}
	resolver := newUserGroupRateResolver(repo, nil, time.Hour, nil, "test")
	require.Equal(t, 30*time.Second, resolver.cacheTTL)
	require.Equal(t, rate, resolver.Resolve(context.Background(), 7, 8, 1))
	svc := &APIKeyService{userGroupRateRepo: repo}
	rates, err := svc.GetUserGroupRates(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, rate, rates[8])
	// Existing gateway token and shared-image billing must use the earned rate.
	groupID := int64(8)
	gw := &GatewayService{userGroupRateResolver: resolver}
	key := &APIKey{GroupID: &groupID, Group: &Group{RateMultiplier: 1}}
	tokenRate, imageRate, _ := gw.recordUsageMultipliers(context.Background(), key, &User{ID: 7}, time.Now())
	require.Equal(t, rate, tokenRate)
	require.Equal(t, rate, imageRate)
	key.Group.ImageRateIndependent, key.Group.ImageRateMultiplier = true, 0.8
	_, imageRate, _ = gw.recordUsageMultipliers(context.Background(), key, &User{ID: 7}, time.Now())
	require.Equal(t, 0.8, imageRate)
}
