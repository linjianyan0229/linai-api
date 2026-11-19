//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTokenCostRequestGPT62UsesOriginalPricingInstant(t *testing.T) {
	svc := NewBillingService(nil, nil)
	for _, tc := range []struct {
		at   time.Time
		want float64
	}{
		{time.Date(2027, 3, 1, 7, 59, 59, 0, time.UTC), 0.006},
		{time.Date(2027, 3, 1, 8, 0, 0, 0, time.UTC), 0.012},
	} {
		cost, err := svc.CalculateTokenCostForRequest(TokenCostRequest{
			Ctx: context.Background(), Model: "gpt-6.2-sol",
			Tokens: UsageTokens{InputTokens: 1000}, RateMultiplier: 0.5,
			ServiceTier: "ultrafast", PricingAt: tc.at,
		})
		require.NoError(t, err)
		require.InDelta(t, tc.want, cost.InputCost, 1e-12)
		require.InDelta(t, tc.want*0.5, cost.ActualCost, 1e-12)
	}
}

func TestOpenAICacheWriteTTLChannelPricingComposesTimeTierAndInterval(t *testing.T) {
	svc := NewBillingService(nil, nil)
	const groupID int64 = 8623
	card := ChannelModelPricing{
		Platform: PlatformOpenAI, Models: []string{"gpt-6.2-sol"}, BillingMode: BillingModeToken,
		InputPrice: float64Ptr(1e-6), OutputPrice: float64Ptr(2e-6),
		CacheWritePrice: float64Ptr(3e-6), CacheWrite12hPrice: float64Ptr(0),
		UltrafastMultiplier: float64Ptr(4),
		Intervals:           []PricingInterval{{MinTokens: 0, CacheWriteMultiplier: float64Ptr(2)}},
		TimePricing: &ChannelTimePricing{Timezone: "UTC", Periods: []ChannelTimePricingPeriod{
			{StartTime: "10:00", EndTime: "14:00", Multiplier: 1.5},
		}},
	}
	resolver := NewModelPricingResolver(newChannelServiceWithPricings(groupID, []ChannelModelPricing{card}), svc)
	tokens := UsageTokens{InputTokens: 100, OutputTokens: 10, CacheCreationTokens: 1000,
		CacheCreation30mTokens: 400, CacheCreation12hTokens: 600}
	for _, tc := range []struct {
		hour int
		time float64
	}{{9, 1}, {12, 1.5}} {
		cost, err := svc.CalculateTokenCostForRequest(TokenCostRequest{
			Ctx: context.Background(), Model: "gpt-6.2-sol", Group: &Group{ID: groupID, Platform: PlatformOpenAI},
			Tokens: tokens, RateMultiplier: 0.5, ServiceTier: "ultrafast", Resolver: resolver,
			PricingAt: time.Date(2026, 11, 19, tc.hour, 0, 0, 0, time.UTC),
		})
		require.NoError(t, err)
		cache := 400 * 3e-6 * 2 * 4 * tc.time // Explicitly free 12h writes stay free.
		want := (100*1e-6+10*2e-6)*4*tc.time + cache
		require.InDelta(t, cache, cost.CacheCreationCost, 1e-12)
		require.InDelta(t, want, cost.TotalCost, 1e-12)
		require.InDelta(t, want*0.5, cost.ActualCost, 1e-12)
	}
	base, err := svc.GetModelPricing("gpt-6.2-sol")
	require.NoError(t, err)
	require.InDelta(t, 5e-6, *base.CacheCreation12hPricePerToken, 1e-15, "operator free prices must not mutate the catalog")
}

func TestOpenAICacheWriteTTLDynamicGroupKeepsFrozenRate(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	manual := 0.7
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, &openAIUserGroupRateRepoStub{rate: &manual})
	now := time.Now().Add(-time.Minute)
	account := dynamicTestAccount(t, 3, 0.08, now)
	group := &Group{ID: 7, Platform: PlatformOpenAI, DynamicRateEnabled: true, DynamicRateFactor: 2,
		RateMultiplier: 0.8, TimeRatePeriods: testGroupTimePeriods(),
		RechargeRateTiers: []GroupRechargeRateTier{{RateMultiplier: 0.5}}}
	quote, err := dynamicRateQuote(group, account, now.Add(time.Second))
	require.NoError(t, err)
	groupID, tier := group.ID, "ultrafast"
	account.Extra = nil // Repricing must not depend on a later probe/config change.
	err = svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{RequestID: "phase3-frozen-ttl", Model: "gpt-6.2-sol", UpstreamModel: "gpt-6.2-sol",
			ServiceTier: &tier, DynamicGroupPricing: quote,
			Usage: OpenAIUsage{InputTokens: 1100, OutputTokens: 10, CacheCreationInputTokens: 1000,
				CacheCreation30mInputTokens: 400, CacheCreation12hInputTokens: 600}},
		APIKey: &APIKey{ID: 2, GroupID: &groupID, Group: group}, User: &User{ID: 1}, Account: account,
		PricingAt: time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	log := usageRepo.lastLog
	require.NotNil(t, log)
	want := (100*2e-6 + 10*10e-6 + 400*2.5e-6 + 600*5e-6) * 3
	require.InDelta(t, want, log.TotalCost, 1e-12)
	require.InDelta(t, want*0.16, userRepo.lastAmount, 1e-12)
	require.InDelta(t, 0.16, log.RateMultiplier, 1e-12)
	require.InDelta(t, 9, *log.AccountRateMultiplier, 1e-12)
	require.Equal(t, 400, log.CacheCreation30mTokens)
	require.Equal(t, 600, log.CacheCreation12hTokens)
	require.Zero(t, log.CacheCreation5mTokens)
	require.Zero(t, log.CacheCreation1hTokens)
}
