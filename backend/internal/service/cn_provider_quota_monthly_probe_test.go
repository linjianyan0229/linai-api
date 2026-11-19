//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"
)

func TestCNProviderQuotaServiceKimiMonthlyProbeClearsMissingWindows(t *testing.T) {
	account := codingAccount(PlatformKimi)
	repo := &cnBalanceProbeRepo{account: account}
	upstream := &cnBalanceResponseUpstream{statusCode: http.StatusOK, body: `{
		"usages": {
			"limit_5h": {"used_ratio": 0.25},
			"limit_month_total": {"used_ratio": 0.8, "reset_time": "2026-12-01T00:00:00Z"},
			"limit_month_code": {"used_ratio": 0.6, "reset_time": "2026-12-05T00:00:00Z"}
		}
	}`}
	svc := NewCNProviderQuotaService(repo, nil, upstream, nil)

	result, err := svc.QueryUsageForAccount(context.Background(), account)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.True(t, result.Persisted)
	require.Len(t, result.Tiers, 2)
	require.Len(t, repo.extraWrites, 1)
	updates := repo.extraWrites[0]
	require.Equal(t, float64(25), updates["kimi_5h_used_percent"])
	require.Contains(t, updates, "kimi_5h_reset_at")
	require.Nil(t, updates["kimi_5h_reset_at"], "an absent reset must clear a stale reset")
	require.Contains(t, updates, "kimi_weekly_used_percent")
	require.Nil(t, updates["kimi_weekly_used_percent"], "a monthly plan must clear the legacy weekly snapshot")
	require.Nil(t, updates["kimi_weekly_reset_at"])
	require.Equal(t, float64(80), updates["kimi_monthly_used_percent"])
	require.Equal(t, "2026-12-05T00:00:00Z", updates["kimi_monthly_reset_at"], "both monthly limits must have reset before the account resumes")
}

func TestCNProviderQuotaServiceKimiUnrecognizedProbePreservesSnapshot(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"usages":{"limit_month_total":{"used_ratio":65}}}`,
		`{"limits":[{"detail":{}}],"usage":{"limit":0,"remaining":0}}`,
	} {
		t.Run(body, func(t *testing.T) {
			account := codingAccount(PlatformKimi)
			repo := &cnBalanceProbeRepo{account: account}
			upstream := &cnBalanceResponseUpstream{statusCode: http.StatusOK, body: body}
			svc := NewCNProviderQuotaService(repo, nil, upstream, nil)

			result, err := svc.QueryUsageForAccount(context.Background(), account)
			require.NoError(t, err)
			require.False(t, result.Success)
			require.False(t, result.Persisted)
			require.Contains(t, result.Error, "no recognized usage window")
			require.Empty(t, repo.extraWrites, "malformed 2xx responses must not erase quota or advance freshness")
		})
	}
}

type cnQuotaIncompleteBodyUpstream struct {
	HTTPUpstream
	body io.Reader
}

func (u *cnQuotaIncompleteBodyUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(u.body)}, nil
}

func TestCNProviderQuotaServiceKimiIncompleteBodyPreservesAllWindows(t *testing.T) {
	validWindow := `{"usages":{"limit_5h":{"used_ratio":0.1}}}`
	for _, tc := range []struct {
		name, want string
		body       io.Reader
	}{
		{"read failure after valid window", "read quota response", io.MultiReader(strings.NewReader(validWindow), iotest.ErrReader(errors.New("fixture read failure")))},
		{"truncated JSON after valid window", "invalid quota response JSON", strings.NewReader(validWindow[:len(validWindow)-1])},
		{"oversized valid JSON", "exceeds", strings.NewReader(`{"padding":"` + strings.Repeat("x", cnQuotaMaxBodyBytes) + `","usages":{"limit_5h":{"used_ratio":0.1}}}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := codingAccount(PlatformKimi)
			repo := &cnBalanceProbeRepo{account: account}
			svc := NewCNProviderQuotaService(repo, nil, &cnQuotaIncompleteBodyUpstream{body: tc.body}, nil)
			result, err := svc.QueryUsageForAccount(context.Background(), account)
			require.NoError(t, err)
			require.False(t, result.Success)
			require.False(t, result.Persisted)
			require.Contains(t, result.Error, tc.want)
			require.Empty(t, repo.extraWrites)
		})
	}
}
