//go:build integration

package repository

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type providerQuotaPlanUpstream struct {
	service.HTTPUpstream
	body string
}

func (u *providerQuotaPlanUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(u.body))}, nil
}

// Exercise the actual probe, JSONB merge, reload and scheduler decision together.
// A schema failure must preserve the old snapshot; a valid plan change must clear
// only the windows that disappeared, without clearing unrelated account settings.
func TestKimiQuotaPlanProbePersistsMonthlyAndClearsAbsentWindows(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	repo := newAccountRepositoryWithSQL(tx.Client(), tx, nil)
	now := time.Now().UTC()
	laterReset := now.Add(14 * 24 * time.Hour).Truncate(time.Second)
	account := mustCreateAccount(t, tx.Client(), &service.Account{
		Name: "kimi-monthly-plan", Platform: service.PlatformKimi, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"account_mode": service.AccountModeCoding, "api_key": "test-only-placeholder", "base_url": "https://api.kimi.com/coding/v1"},
		Extra: map[string]any{
			"kimi_weekly_used_percent": float64(99), "kimi_weekly_reset_at": now.Add(7 * 24 * time.Hour).Format(time.RFC3339),
			"billing_note": "keep",
		},
	})
	upstream := &providerQuotaPlanUpstream{body: `{"usages":{
		"limit_5h":{"used_ratio":0.25},
		"limit_month_total":{"used_ratio":0.91,"reset_time":"` + now.Add(10*24*time.Hour).Format(time.RFC3339) + `"},
		"limit_month_code":{"used_ratio":0.4,"reset_time":"` + laterReset.Format(time.RFC3339) + `"}
	}}`}
	probe := service.NewCNProviderQuotaService(repo, nil, upstream, nil)
	result, err := probe.QueryUsageForAccount(ctx, account)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.True(t, result.Persisted)
	loaded, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Nil(t, loaded.Extra["kimi_weekly_used_percent"])
	require.Nil(t, loaded.Extra["kimi_weekly_reset_at"])
	require.InDelta(t, 91, loaded.Extra["kimi_monthly_used_percent"], 1e-9)
	require.Equal(t, laterReset.Format(time.RFC3339), loaded.Extra["kimi_monthly_reset_at"])
	require.Equal(t, "keep", loaded.Extra["billing_note"])
	decision := service.EvaluateAccountSchedulingThreshold(loaded, map[string]int{service.PlatformKimi: 80}, now)
	require.True(t, decision.ShouldPause)
	require.Equal(t, "monthly", decision.Window)
	require.NotNil(t, decision.Until)
	require.True(t, laterReset.Equal(*decision.Until))

	upstream.body = `{}`
	result, err = probe.QueryUsageForAccount(ctx, loaded)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.False(t, result.Persisted)
	afterFailedProbe, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, loaded.Extra, afterFailedProbe.Extra, "an unrecognized response must not erase an exhausted snapshot")

	upstream.body = `{"limits":[{"detail":{"limit":100,"remaining":90}}]}`
	result, err = probe.QueryUsageForAccount(ctx, afterFailedProbe)
	require.NoError(t, err)
	require.True(t, result.Persisted)
	afterLegacyPlan, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Nil(t, afterLegacyPlan.Extra["kimi_monthly_used_percent"])
	require.Nil(t, afterLegacyPlan.Extra["kimi_monthly_reset_at"])
	require.Equal(t, "keep", afterLegacyPlan.Extra["billing_note"])
	require.False(t, service.EvaluateAccountSchedulingThreshold(afterLegacyPlan, map[string]int{service.PlatformKimi: 80}, now).ShouldPause)
}

func TestVertexMultiRegionCredentialsSurviveRepositoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	repo := newAccountRepositoryWithSQL(tx.Client(), tx, nil)
	for _, region := range []string{"us", "eu"} {
		t.Run(region, func(t *testing.T) {
			account := mustCreateAccount(t, tx.Client(), &service.Account{
				Name: "vertex-" + region, Platform: service.PlatformGemini, Type: service.AccountTypeServiceAccount,
				Credentials: map[string]any{
					"auth_mode": "apikey", "api_key": "test-only-placeholder", "project_id": "fixture-project", "location": region,
					"vertex_model_locations": map[string]any{"gemini-custom": "europe-west4"},
				},
			})
			loaded, err := repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.Equal(t, region, loaded.VertexLocation("gemini-3.7-flash"))
			require.Equal(t, "global", loaded.VertexLocation("gemini-3.1-pro-preview"))
			require.Equal(t, "europe-west4", loaded.VertexLocation("gemini-custom"))
			require.Equal(t, 1.1, service.VertexUpstreamPriceMultiplier(loaded, "gemini-3.7-flash", ""))
			require.Equal(t, float64(1), service.VertexUpstreamPriceMultiplier(loaded, "gemini-3.7-flash", "global"))
		})
	}
}
