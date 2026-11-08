package service

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func dynamicTestAccount(t *testing.T, id int64, rate float64, now time.Time) *Account {
	t.Helper()
	expires := now.Add(time.Hour)
	snapshot := UpstreamBillingProbeSnapshot{Status: UpstreamBillingProbeStatusOK, ReceivedAt: &now, FreshUntil: &expires,
		Data: map[string]any{"billing_scope": "token", "resolved_rate_multiplier": rate, "peak_rate_enabled": false}}
	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	var encoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &encoded))
	manual := 9.0 // A manual account rate must not override the authenticated quote.
	return &Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, RateMultiplier: &manual,
		Extra: map[string]any{UpstreamBillingProbeEnabledExtraKey: true, UpstreamBillingProbeExtraKey: encoded}}
}

func TestGroupDynamicRateValidationAndExpiry(t *testing.T) {
	for _, factor := range []float64{0, -1, math.NaN(), math.Inf(1), 10000, 1.12345} {
		require.Error(t, ValidateGroupDynamicRate(true, factor, PlatformOpenAI))
	}
	require.Error(t, ValidateGroupDynamicRate(true, 2, PlatformAnthropic))
	require.NoError(t, ValidateGroupDynamicRate(true, 2, PlatformOpenAI))
	now := time.Now().Add(-time.Minute)
	for _, change := range []func(*Account){
		func(a *Account) { a.Extra[UpstreamBillingProbeEnabledExtraKey] = false },
		func(a *Account) { a.Type = AccountTypeOAuth },
		func(a *Account) { a.Platform = PlatformAnthropic },
		func(a *Account) { delete(a.Extra, UpstreamBillingProbeExtraKey) },
		func(a *Account) {
			snapshot, ok := a.Extra[UpstreamBillingProbeExtraKey].(map[string]any)
			require.True(t, ok)
			snapshot["status"] = UpstreamBillingProbeStatusFailed
		},
		func(a *Account) {
			snapshot, ok := a.Extra[UpstreamBillingProbeExtraKey].(map[string]any)
			require.True(t, ok)
			snapshot["status"] = UpstreamBillingProbeStatusUnsupported
		},
	} {
		a := dynamicTestAccount(t, 3, .08, now)
		change(a)
		_, ok := DynamicUpstreamBillingRate(a, now.Add(time.Second))
		require.False(t, ok)
	}
	a := dynamicTestAccount(t, 3, .08, now)
	_, ok := DynamicUpstreamBillingRate(a, now.Add(time.Hour-time.Nanosecond))
	require.True(t, ok)
	_, ok = DynamicUpstreamBillingRate(a, now.Add(time.Hour))
	require.False(t, ok, "expiry boundary is exclusive")
	_, ok = DynamicUpstreamBillingRate(a, now.Add(-time.Second))
	require.False(t, ok)
	zero := dynamicTestAccount(t, 3, 0, now)
	rate, ok := DynamicUpstreamBillingRate(zero, now.Add(time.Second))
	require.True(t, ok, "an explicitly quoted free upstream is valid")
	require.Zero(t, rate)
}

func TestGroupDynamicRateFrozenAttemptsAndOverrides(t *testing.T) {
	now := time.Now().Add(-time.Minute)
	group := &Group{ID: 7, Platform: PlatformOpenAI, DynamicRateEnabled: true, DynamicRateFactor: 2,
		RateMultiplier: .8, RechargeRateTiers: []GroupRechargeRateTier{{RateMultiplier: .5}},
		TimeRatePeriods: testGroupTimePeriods(), PeakRateEnabled: true, PeakStart: "00:00", PeakEnd: "23:59", PeakRateMultiplier: 3}
	first := dynamicTestAccount(t, 3, .08, now)
	quote, err := dynamicRateQuote(group, first, now.Add(time.Second))
	require.NoError(t, err)
	require.InDelta(t, .16, quote.Rate, 1e-12)
	require.Equal(t, .5, group.TimeAdjustedRate(.5, now), "time pricing is paused")
	require.Equal(t, 1.0, group.PeakMultiplierAt(now), "legacy peak is paused")
	first.Extra = nil // A long-running request remains billed at its frozen quote.
	rate, active, err := dynamicUsageRate(group, first, quote)
	require.NoError(t, err)
	require.True(t, active)
	require.InDelta(t, .16, rate, 1e-12)
	second := dynamicTestAccount(t, 4, .1, now)
	newQuote, err := dynamicRateQuote(group, second, now.Add(time.Second))
	require.NoError(t, err)
	require.InDelta(t, .2, newQuote.Rate, 1e-12)
	_, _, err = dynamicUsageRate(group, second, quote)
	require.Error(t, err, "a failed attempt cannot price a different successful upstream")
	_, _, err = dynamicUsageRate(group, second, nil)
	require.Error(t, err, "never fall back to static pricing after forwarding")
	group.DynamicRateEnabled = false
	_, active, err = dynamicUsageRate(group, second, nil)
	require.NoError(t, err)
	require.False(t, active)
	require.NotEmpty(t, group.TimeRatePeriods, "configuration is preserved")
}

func TestGroupDynamicRateAdmissionWithoutProfitGate(t *testing.T) {
	group := &Group{ID: 7, Platform: PlatformOpenAI, DynamicRateEnabled: true, DynamicRateFactor: 2}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)
	bad := &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	vetoed, reason := openAIProfitControlVetoReason(ctx, bad)
	require.True(t, vetoed)
	require.Equal(t, "dynamic_rate_unavailable", reason)
	good := dynamicTestAccount(t, 4, .08, time.Now().Add(-time.Minute))
	vetoed, _ = openAIProfitControlVetoReason(ctx, good)
	require.False(t, vetoed)
	ctx = context.WithValue(ctx, openAIProfitControlGateCtxKey{}, &openAIProfitControlGate{dynamicFactor: 2, dynamicMargin: .9})
	vetoed, _ = openAIProfitControlVetoReason(ctx, good)
	require.True(t, vetoed, "operator-configured account cost remains authoritative for profit protection")
	configuredCost := .08
	good.RateMultiplier = &configuredCost
	vetoed, _ = openAIProfitControlVetoReason(ctx, good)
	require.False(t, vetoed)
	ctx = context.WithValue(ctx, openAIProfitControlGateCtxKey{}, &openAIProfitControlGate{dynamicFactor: .5, dynamicMargin: .9})
	vetoed, _ = openAIProfitControlVetoReason(ctx, good)
	require.True(t, vetoed)
}

func TestGroupDynamicRateRecordUsageChargesFrozenPrice(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	manual := .7
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, &openAIUserGroupRateRepoStub{rate: &manual})
	now := time.Now().Add(-time.Minute)
	account := dynamicTestAccount(t, 3, .08, now)
	group := &Group{ID: 7, Platform: PlatformOpenAI, DynamicRateEnabled: true, DynamicRateFactor: 2, RateMultiplier: .8, TimeRatePeriods: testGroupTimePeriods()}
	quote, err := dynamicRateQuote(group, account, now.Add(time.Second))
	require.NoError(t, err)
	groupID := group.ID
	user := &User{ID: 1}
	key := &APIKey{ID: 2, UserID: user.ID, User: user, GroupID: &groupID, Group: group}
	usage := OpenAIUsage{InputTokens: 1200, OutputTokens: 300}
	result := &OpenAIForwardResult{RequestID: "dynamic-frozen", Model: "gpt-5.1", Usage: usage, DynamicGroupPricing: quote}
	account.Extra = nil // Probe expiry/config changes after sending must not reprice usage.
	err = svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, APIKey: key, User: user, Account: account})
	require.NoError(t, err)
	expected := expectedOpenAICost(t, svc, result.Model, usage, .16)
	require.InDelta(t, expected.ActualCost, userRepo.lastAmount, 1e-12)
	require.InDelta(t, .16, usageRepo.lastLog.RateMultiplier, 1e-12)
	require.InDelta(t, 9.0, *usageRepo.lastLog.AccountRateMultiplier, 1e-12)
	result.DynamicGroupPricing = nil
	err = svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, APIKey: key, User: user, Account: account})
	require.Error(t, err)
	require.Equal(t, 1, userRepo.deductCalls)
}

func TestGroupDynamicRateProfitPreview(t *testing.T) {
	now := time.Now().Add(-time.Minute)
	g := &Group{ID: 7, Platform: PlatformOpenAI, DynamicRateEnabled: true, DynamicRateFactor: 2, RateMultiplier: 9, ProfitControlEnabled: true, ProfitMinMargin: .1}
	a, b := dynamicTestAccount(t, 3, .08, now), dynamicTestAccount(t, 4, .1, now)
	costA, costB := .08, .1
	a.RateMultiplier, b.RateMultiplier = &costA, &costB
	missing := &Account{ID: 5, Platform: PlatformOpenAI, RateMultiplier: a.RateMultiplier}
	reports := PreviewProfitAdmission([]ProfitPreviewGroupInput{{Group: g, Accounts: []*Account{a, b, missing}, UserOverrides: map[int64]float64{1: .01}}}, now.Add(time.Second))
	require.Len(t, reports, 1)
	report := reports[0]
	require.True(t, report.DynamicRateEnabled)
	require.InDelta(t, .2, report.DefaultD, 1e-12)
	require.InDelta(t, .16, report.MinEffectiveD, 1e-12)
	require.Equal(t, ProfitPreviewClassAdmitted, report.Verdicts[0].Class)
	require.InDelta(t, .16, *report.Verdicts[0].DynamicFinalRate, 1e-12)
	require.InDelta(t, .144, *report.Verdicts[0].DynamicThreshold, 1e-12)
	require.Equal(t, ProfitPreviewClassRejectedInvalidRate, report.Verdicts[2].Class)
}
