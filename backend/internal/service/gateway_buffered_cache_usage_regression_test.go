//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBufferedAnthropicConversionsPreserveCacheOnlyFinalUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"responses", "chat"} {
		for _, tc := range []struct {
			name     string
			start    string
			delta    string
			creation int
			read     int
		}{
			{"cache writes", `"cache_creation":{"ephemeral_5m_input_tokens":20}`, `"cache_creation":{"ephemeral_1h_input_tokens":30}`, 50, 0},
			{"cache reads", `"cache_read_input_tokens":10`, `"cache_read_input_tokens":20`, 0, 20},
		} {
			t.Run(protocol+"/"+tc.name, func(t *testing.T) {
				payload := "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"msg_cache","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4","usage":{"input_tokens":0,"output_tokens":0,` + tc.start + `}}}` + "\n\n" +
					"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":0,"output_tokens":0,` + tc.delta + `}}` + "\n\n" +
					"event: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n"
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, nil)
				resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}
				svc := &GatewayService{}
				var result *ForwardResult
				var err error
				if protocol == "responses" {
					result, err = svc.handleResponsesBufferedStreamingResponse(resp, c, "claude-sonnet-4", "claude-sonnet-4", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
				} else {
					result, err = svc.handleCCBufferedFromAnthropic(resp, c, "claude-sonnet-4", "claude-sonnet-4", nil, time.Now())
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Zero(t, result.Usage.InputTokens)
				require.Zero(t, result.Usage.OutputTokens)
				require.Equal(t, tc.creation, result.Usage.CacheCreationInputTokens)
				require.Equal(t, tc.read, result.Usage.CacheReadInputTokens)
				body := gjson.Parse(recorder.Body.String())
				if protocol == "responses" {
					require.EqualValues(t, tc.creation+tc.read, body.Get("usage.input_tokens").Int())
					require.EqualValues(t, tc.creation, body.Get("usage.cache_creation_input_tokens").Int())
					require.EqualValues(t, tc.read, body.Get("usage.input_tokens_details.cached_tokens").Int())
				} else {
					require.EqualValues(t, tc.creation+tc.read, body.Get("usage.prompt_tokens").Int())
					require.EqualValues(t, tc.creation, body.Get("usage.prompt_tokens_details.cache_creation_tokens").Int())
					require.EqualValues(t, tc.read, body.Get("usage.prompt_tokens_details.cached_tokens").Int())
				}
			})
		}
	}
}

func TestGatewayRecordUsageAccountStatsPreservesCacheTTL(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{})
	groupID := int64(7)
	customer5m, customer1h, stats5m, stats1h := 0.001, 0.003, 0.002, 0.004
	group := &Group{ID: groupID, Platform: PlatformAnthropic, RateMultiplier: 1.7,
		ModelPricing: []ChannelModelPricing{{Models: []string{"claude-sonnet-4"}, BillingMode: BillingModeToken, CacheWritePrice: &customer5m, CacheWrite1hPrice: &customer1h}},
	}
	svc.channelService = newTestChannelServiceForStats(t, &Channel{
		ID: 1, Status: StatusActive,
		AccountStatsPricingRules: []AccountStatsPricingRule{{
			GroupIDs: []int64{groupID},
			Pricing:  []ChannelModelPricing{{Models: []string{"claude-sonnet-4"}, BillingMode: BillingModeToken, CacheWritePrice: &stats5m, CacheWrite1hPrice: &stats1h}},
		}},
	}, groupID, PlatformAnthropic)
	svc.resolver = NewModelPricingResolver(svc.channelService, svc.billingService)
	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{RequestID: "cache-ttl-account-stats", Model: "claude-sonnet-4", Usage: ClaudeUsage{
			CacheCreationInputTokens: 100, CacheCreation5mTokens: 20, CacheCreation1hTokens: 30,
		}},
		APIKey: &APIKey{ID: 8, GroupID: &groupID, Group: group}, User: &User{ID: 6},
		Account: &Account{ID: 9, Platform: PlatformAnthropic},
	})
	require.NoError(t, err)
	log := usageRepo.lastLog
	require.NotNil(t, log)
	require.Equal(t, 100, log.CacheCreationTokens)
	require.Equal(t, 20, log.CacheCreation5mTokens)
	require.Equal(t, 30, log.CacheCreation1hTokens)
	require.NotNil(t, log.AccountStatsCost)
	require.InDelta(t, 70*stats5m+30*stats1h, *log.AccountStatsCost, 1e-12)
	require.InDelta(t, 70*customer5m+30*customer1h, log.TotalCost, 1e-12)
	require.InDelta(t, (70*customer5m+30*customer1h)*1.7, log.ActualCost, 1e-12)
	require.InDelta(t, log.ActualCost, userRepo.lastAmount, 1e-12, "account statistics must not change the customer rate or deduction")
}
