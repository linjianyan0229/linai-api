package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testGroupTimePeriods() []GroupTimeRatePeriod {
	return []GroupTimeRatePeriod{{Start: "08:00", End: "20:00", RateMultiplier: 1.2}, {Start: "20:00", End: "08:00", RateMultiplier: 0.6}}
}

func TestGroupTimeRateValidation(t *testing.T) {
	require.NoError(t, ValidateGroupTimeRatePeriods(nil, true))
	require.NoError(t, ValidateGroupTimeRatePeriods(testGroupTimePeriods(), false))
	require.Error(t, ValidateGroupTimeRatePeriods(testGroupTimePeriods(), true))
	for name, change := range map[string]func([]GroupTimeRatePeriod) []GroupTimeRatePeriod{
		"one period":      func(p []GroupTimeRatePeriod) []GroupTimeRatePeriod { return p[:1] },
		"overlap":         func(p []GroupTimeRatePeriod) []GroupTimeRatePeriod { p[1].Start = "19:00"; return p },
		"gap":             func(p []GroupTimeRatePeriod) []GroupTimeRatePeriod { p[1].Start = "21:00"; return p },
		"same boundaries": func(p []GroupTimeRatePeriod) []GroupTimeRatePeriod { p[0].End = "08:00"; return p },
		"bad time":        func(p []GroupTimeRatePeriod) []GroupTimeRatePeriod { p[0].Start = "24:00"; return p },
		"negative":        func(p []GroupTimeRatePeriod) []GroupTimeRatePeriod { p[0].RateMultiplier = -1; return p },
		"NaN":             func(p []GroupTimeRatePeriod) []GroupTimeRatePeriod { p[0].RateMultiplier = math.NaN(); return p },
		"infinite":        func(p []GroupTimeRatePeriod) []GroupTimeRatePeriod { p[0].RateMultiplier = math.Inf(1); return p },
		"precision":       func(p []GroupTimeRatePeriod) []GroupTimeRatePeriod { p[0].RateMultiplier = 0.12345; return p },
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, ValidateGroupTimeRatePeriods(change(testGroupTimePeriods()), false))
		})
	}
}

func TestGroupTimeRateBoundariesAndFormula(t *testing.T) {
	g := &Group{SubscriptionType: SubscriptionTypeStandard, RateMultiplier: 0.8, TimeRatePeriods: testGroupTimePeriods()}
	for _, tc := range []struct {
		hour, minute int
		want         float64
	}{
		{0, 0, 0.6}, {7, 59, 0.6}, {8, 0, 1.2}, {19, 59, 1.2}, {20, 0, 0.6}, {23, 59, 0.6},
	} {
		require.InDelta(t, tc.want, g.TimeAdjustedRate(0.8, at(tc.hour, tc.minute)), 1e-9)
	}
	// Caller-local timezone must not change the server's pricing instant.
	require.InDelta(t, 0.6, g.TimeAdjustedRate(0.8, at(7, 59).In(mustLoad("Asia/Shanghai"))), 1e-9)
	g.RechargeRateTiers = testRechargeTiers()
	require.InDelta(t, 0.75*1.2, g.TimeAdjustedRate(0.75, at(8, 0)), 1e-9)
	require.InDelta(t, 0.5*0.6, g.TimeAdjustedRate(0.5, at(20, 0)), 1e-9)
	g.TimeRatePeriods = nil
	require.Equal(t, 0.75, g.TimeAdjustedRate(0.75, at(8, 0)))
}

func TestGroupTimeRateBillingAndSnapshot(t *testing.T) {
	rate := 0.75
	repo := &rechargeRateTestRepo{userGroupRateResolverRepoStub: userGroupRateResolverRepoStub{rate: &rate}}
	groupID := int64(8)
	key := &APIKey{User: &User{ID: 7}, GroupID: &groupID, Group: &Group{
		ID: groupID, RateMultiplier: 0.8, RechargeRateTiers: testRechargeTiers(), TimeRatePeriods: testGroupTimePeriods(),
	}}
	svc := &APIKeyService{}
	restored := svc.snapshotToAPIKey("test", svc.snapshotFromAPIKey(context.Background(), key))
	require.Equal(t, key.Group.TimeRatePeriods, restored.Group.TimeRatePeriods)
	require.Equal(t, key.Group.RechargeRateTiers, restored.Group.RechargeRateTiers)
	gw := &GatewayService{userGroupRateResolver: newUserGroupRateResolver(repo, nil, time.Minute, nil, "test")}
	for _, tc := range []struct {
		instant time.Time
		want    float64
	}{{at(19, 59), 0.9}, {at(20, 0), 0.45}} {
		text, image, instant := gw.recordUsageMultipliers(context.Background(), restored, key.User, tc.instant)
		require.InDelta(t, tc.want, text, 1e-9)
		require.InDelta(t, tc.want, image, 1e-9)
		require.Equal(t, tc.instant, instant)
		require.InDelta(t, text, restored.Group.EffectiveTokenRate(rate, instant), 1e-9)
	}
	// Independent media rates remain independent from daily group prices.
	restored.Group.ImageRateIndependent, restored.Group.ImageRateMultiplier = true, 0.4
	text, image, _ := gw.recordUsageMultipliers(context.Background(), restored, key.User, at(8, 0))
	require.InDelta(t, 0.9, text, 1e-9)
	require.Equal(t, 0.4, image)
}

func TestGroupTimeRateUpstreamProbe(t *testing.T) {
	body := []byte(`{"object":"sub2api.key_billing","schema_version":1,"billing_scope":"token","group_rate_multiplier":0.8,"resolved_rate_multiplier":0.8,"peak_rate_enabled":false,"effective_rate_multiplier":1.2,"timezone":"UTC","observed_at":"2026-06-29T19:59:00Z","time_rate_periods":[{"start":"08:00","end":"20:00","rate_multiplier":1.2},{"start":"20:00","end":"08:00","rate_multiplier":0.6}]}`)
	data, err := parseUpstreamBillingProbeResponse(body)
	require.NoError(t, err)
	rate, ok := upstreamBillingRateAt(data, at(20, 0))
	require.True(t, ok)
	require.Equal(t, 0.6, rate, "remote time rules are recalculated after the probe instant")
	data["recharge_rate_enabled"] = true
	rate, ok = upstreamBillingRateAt(data, at(20, 0))
	require.True(t, ok)
	require.InDelta(t, 0.8*0.6, rate, 1e-9)
}

func TestGroupTimeRateProfitPreview(t *testing.T) {
	group := &Group{ID: 8, Platform: PlatformOpenAI, RateMultiplier: 0.8, RechargeRateTiers: testRechargeTiers(), TimeRatePeriods: testGroupTimePeriods()}
	report := PreviewProfitAdmission([]ProfitPreviewGroupInput{{Group: group}}, at(8, 0))
	require.Len(t, report, 1)
	require.InDelta(t, 1.2, report[0].DefaultD, 1e-9)
	require.InDelta(t, 0.6, report[0].MinEffectiveD, 1e-9)
	group.RechargeRateTiers = nil
	report = PreviewProfitAdmission([]ProfitPreviewGroupInput{{Group: group, UserOverrides: map[int64]float64{1: 0.3}}}, at(8, 0))
	require.InDelta(t, 1.2, report[0].DefaultD, 1e-9)
	require.InDelta(t, 1.2, report[0].MinEffectiveD, 1e-9)
}
