//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCacheCreationBillingAccountsForUnclassifiedTokens(t *testing.T) {
	pricing := &ModelPricing{SupportsCacheBreakdown: true, CacheCreation5mPrice: 0.002, CacheCreation1hPrice: 0.004}
	price5m, price1h := 0.002, 0.004
	statsPricing := &ChannelModelPricing{CacheWritePrice: &price5m, CacheWrite1hPrice: &price1h}
	for _, tc := range []struct {
		name           string
		tokens         UsageTokens
		want5m, want1h int
	}{
		{"partial details", UsageTokens{CacheCreationTokens: 100, CacheCreation5mTokens: 20, CacheCreation1hTokens: 30}, 70, 30},
		{"aggregate only", UsageTokens{CacheCreationTokens: 100}, 100, 0},
		{"details only", UsageTokens{CacheCreation5mTokens: 20, CacheCreation1hTokens: 30}, 20, 30},
		{"oversized details", UsageTokens{CacheCreationTokens: 100, CacheCreation5mTokens: 50, CacheCreation1hTokens: 200}, 20, 80},
		{"negative detail", UsageTokens{CacheCreationTokens: 100, CacheCreation5mTokens: -20, CacheCreation1hTokens: 30}, 70, 30},
		{"empty", UsageTokens{}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got5m, got1h := allocateCacheCreationBillingTokens(tc.tokens)
			require.Equal(t, tc.want5m, got5m)
			require.Equal(t, tc.want1h, got1h)
			want := float64(tc.want5m)*price5m + float64(tc.want1h)*price1h
			cost := (&BillingService{}).computeCacheCreationCost(pricing, tc.tokens, price5m, 1.5)
			require.InDelta(t, want*1.5, cost, 1e-12)
			statsCost := calculateTokenStatsCost(statsPricing, tc.tokens)
			if want == 0 {
				require.Nil(t, statsCost)
			} else {
				require.NotNil(t, statsCost)
				require.InDelta(t, want, *statsCost, 1e-12, "account stats must use the same complete TTL allocation")
			}
		})
	}
}

func TestStandardSSEUsageRetainsCacheSnapshotsAndPromptTotal(t *testing.T) {
	svc := &GatewayService{}
	usage := &ClaudeUsage{}
	svc.parseSSEUsage(`{"type":"message_start","message":{"usage":{"input_tokens":100,"prompt_tokens":100,"cache_read_input_tokens":20,"cache_creation_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":10,"ephemeral_1h_input_tokens":20}}}}`, usage)
	require.Equal(t, 50, usage.InputTokens)
	svc.parseSSEUsage(`{"type":"message_delta","usage":{"cache_read_input_tokens":25,"cache_creation_input_tokens":40}}`, usage)
	require.Equal(t, 35, usage.InputTokens, "cache-only deltas must re-split the retained prompt total")
	require.Equal(t, 40, usage.CacheCreationInputTokens)
	require.Equal(t, 10, usage.CacheCreation5mTokens)
	require.Equal(t, 20, usage.CacheCreation1hTokens)
	svc.parseSSEUsage(`{"type":"message_delta","usage":{"input_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":7}}`, usage)
	require.Equal(t, 35, usage.InputTokens)
	require.Equal(t, 40, usage.CacheCreationInputTokens, "zero placeholders must not erase a previous cumulative total")
	require.Equal(t, 25, usage.CacheReadInputTokens)
	require.Equal(t, 7, usage.OutputTokens)
	got5m, got1h := allocateCacheCreationBillingTokens(UsageTokens{CacheCreationTokens: usage.CacheCreationInputTokens, CacheCreation5mTokens: usage.CacheCreation5mTokens, CacheCreation1hTokens: usage.CacheCreation1hTokens})
	require.Equal(t, 20, got5m)
	require.Equal(t, 20, got1h)
}

func TestStandardSSEUsageMergesPartialCacheTTLs(t *testing.T) {
	svc := &GatewayService{}
	usage := &ClaudeUsage{}
	svc.parseSSEUsage(`{"type":"message_start","message":{"usage":{"input_tokens":10,"cache_creation_input_tokens":null,"cache_creation":{"ephemeral_5m_input_tokens":20}}}}`, usage)
	require.Equal(t, 20, usage.CacheCreationInputTokens)
	svc.parseSSEUsage(`{"type":"message_delta","usage":{"cache_creation":{"ephemeral_1h_input_tokens":30}}}`, usage)
	require.Equal(t, 50, usage.CacheCreationInputTokens)
	svc.parseSSEUsage(`{"type":"message_delta","usage":{"cache_creation_input_tokens":null,"cache_creation":{"ephemeral_5m_input_tokens":null,"ephemeral_1h_input_tokens":30}}}`, usage)
	require.Equal(t, 50, usage.CacheCreationInputTokens, "repeated cumulative snapshots and nulls must not add or erase tokens")
	svc.parseSSEUsage(`{"type":"message_delta","usage":{"cache_creation":{"ephemeral_5m_input_tokens":0}}}`, usage)
	require.Equal(t, 30, usage.CacheCreationInputTokens)
	require.Zero(t, usage.CacheCreation5mTokens)
	require.Equal(t, 30, usage.CacheCreation1hTokens)
	require.Equal(t, 10, usage.InputTokens, "native Anthropic input is already an exclusive bucket")
}

func TestNativeBufferedUsagePreservesPartialTTLAndBillingTotal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	body := `{"id":"msg_cache","type":"message","model":"claude-sonnet-4","content":[],"stop_reason":"end_turn","usage":{"input_tokens":150,"prompt_tokens":150,"output_tokens":7,"cache_read_input_tokens":20,"cache_creation_input_tokens":100,"cache_creation":{"ephemeral_5m_input_tokens":20,"ephemeral_1h_input_tokens":30}}}`
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
	svc := &GatewayService{cfg: &config.Config{}, rateLimitService: &RateLimitService{}}
	usage, err := svc.handleNonStreamingResponse(context.Background(), resp, c, &Account{ID: 701, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}, "claude-sonnet-4", "claude-sonnet-4")
	require.NoError(t, err)
	require.Equal(t, 30, usage.InputTokens)
	require.Equal(t, 100, usage.CacheCreationInputTokens)
	require.Equal(t, 20, usage.CacheCreation5mTokens)
	require.Equal(t, 30, usage.CacheCreation1hTokens)
	require.True(t, usage.cacheCreationInputTokensPresent)
	cost := (&BillingService{}).computeCacheCreationCost(&ModelPricing{SupportsCacheBreakdown: true, CacheCreation5mPrice: 0.002, CacheCreation1hPrice: 0.004}, UsageTokens{CacheCreationTokens: usage.CacheCreationInputTokens, CacheCreation5mTokens: usage.CacheCreation5mTokens, CacheCreation1hTokens: usage.CacheCreation1hTokens}, 0.002, 1)
	require.InDelta(t, 0.26, cost, 1e-12, "the 50 unclassified tokens must be charged at the 5m price")
}

func TestCacheTTLOverrideUsesAggregateRatherThanPartialDetails(t *testing.T) {
	for _, target := range []string{"5m", "1h"} {
		t.Run(target, func(t *testing.T) {
			usage := &ClaudeUsage{CacheCreationInputTokens: 100, CacheCreation5mTokens: 20, CacheCreation1hTokens: 30, cacheCreationInputTokensPresent: true}
			require.True(t, applyCacheTTLOverride(usage, target))
			if target == "1h" {
				require.Equal(t, 100, usage.CacheCreation1hTokens)
				require.Zero(t, usage.CacheCreation5mTokens)
			} else {
				require.Equal(t, 100, usage.CacheCreation5mTokens)
				require.Zero(t, usage.CacheCreation1hTokens)
			}
			require.False(t, applyCacheTTLOverride(usage, target), "reapplying an override must be idempotent")
			jsonUsage := map[string]any{"cache_creation_input_tokens": float64(100), "cache_creation": map[string]any{"ephemeral_5m_input_tokens": float64(20), "ephemeral_1h_input_tokens": float64(30)}}
			require.True(t, rewriteCacheCreationJSON(jsonUsage, target))
			details := jsonUsage["cache_creation"].(map[string]any)
			require.Equal(t, float64(100), details["ephemeral_5m_input_tokens"].(float64)+details["ephemeral_1h_input_tokens"].(float64))
			require.False(t, rewriteCacheCreationJSON(map[string]any{"cache_creation_input_tokens": float64(100)}, target), "do not synthesize TTL fields on an otherwise untouched response")
		})
	}
}
