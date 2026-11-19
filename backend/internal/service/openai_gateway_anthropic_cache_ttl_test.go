//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Exercise the native Anthropic response parser, the OpenAI result boundary,
// actual RecordUsage billing, and the stored usage row together.
func TestOpenAINativeAnthropicCacheTTLRecordUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name        string
		cacheUsage  string
		aggregate   int
		cache5m     int
		cache1h     int
		cacheCost   float64
		placeholder bool
	}{
		{"one hour", `"cache_creation_input_tokens":50,"cache_creation":{"ephemeral_1h_input_tokens":50}`, 50, 0, 50, 50 * 7e-6, false},
		{"unclassified remainder uses five minutes", `"cache_creation_input_tokens":100,"cache_creation":{"ephemeral_5m_input_tokens":20,"ephemeral_1h_input_tokens":30}`, 100, 20, 30, 70*3e-6 + 30*7e-6, false},
		{"TTL details supply missing aggregate", `"cache_creation":{"ephemeral_5m_input_tokens":20,"ephemeral_1h_input_tokens":30}`, 50, 20, 30, 20*3e-6 + 30*7e-6, false},
		{"zero placeholders retain previous usage", `"cache_creation_input_tokens":50,"cache_creation":{"ephemeral_1h_input_tokens":50}`, 50, 0, 50, 50 * 7e-6, true},
	} {
		for _, stream := range []bool{false, true} {
			if tc.placeholder && !stream {
				continue
			}
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, stream), func(t *testing.T) {
				const model = "claude-cache-ttl-test"
				usageJSON := `{"input_tokens":100,"output_tokens":5,"cache_read_input_tokens":10,` + tc.cacheUsage + `}`
				payload := `{"id":"msg_ttl","type":"message","role":"assistant","model":"` + model + `","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":` + usageJSON + `}`
				contentType := "application/json"
				if stream {
					payload = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_ttl\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"" + model + "\",\"content\":[],\"usage\":" + usageJSON + "}}\n\n"
					payload += "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"
					delta := `{"output_tokens":5}`
					if tc.placeholder {
						delta = `{"input_tokens":0,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}`
					}
					payload += "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":" + delta + "}\n\n"
					payload += "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
					contentType = "text/event-stream"
				}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(payload))}
				account := &Account{ID: 704, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
				forward := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
				var result *OpenAIForwardResult
				var err error
				if stream {
					result, err = forward.handleNativeAnthropicStreamingResponse(context.Background(), resp, c, account, model, model, model, nil, time.Now())
				} else {
					result, err = forward.handleNativeAnthropicBufferedResponse(context.Background(), resp, c, account, model, model, model, nil, time.Now())
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, 110+tc.aggregate, result.Usage.InputTokens, "OpenAI total input includes both cache kinds")
				require.Equal(t, tc.aggregate, result.Usage.CacheCreationInputTokens)
				require.Equal(t, tc.cache5m, result.Usage.CacheCreation5mTokens)
				require.Equal(t, tc.cache1h, result.Usage.CacheCreation1hTokens)

				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				users := &openAIRecordUsageUserRepoStub{}
				svc := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
				svc.billingService = NewBillingService(svc.cfg, &PricingService{pricingData: map[string]*LiteLLMModelPricing{
					model: {
						InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, CacheReadInputTokenCost: 0.1e-6,
						CacheCreationInputTokenCost: 3e-6, CacheCreationInputTokenCostAbove1hr: 7e-6,
					},
				}})
				err = svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
					Result: result, APIKey: &APIKey{ID: 705}, User: &User{ID: 706}, Account: account,
				})
				require.NoError(t, err)
				require.NotNil(t, logs.lastLog)
				require.Equal(t, 100, logs.lastLog.InputTokens, "cache writes must not be billed again as ordinary input")
				require.Equal(t, tc.aggregate, logs.lastLog.CacheCreationTokens)
				require.Equal(t, tc.cache5m, logs.lastLog.CacheCreation5mTokens)
				require.Equal(t, tc.cache1h, logs.lastLog.CacheCreation1hTokens)
				require.InDelta(t, tc.cacheCost, logs.lastLog.CacheCreationCost, 1e-12)
				total := 100*1e-6 + 5*2e-6 + 10*0.1e-6 + tc.cacheCost
				require.InDelta(t, total, logs.lastLog.TotalCost, 1e-12)
				require.InDelta(t, total*1.1, logs.lastLog.ActualCost, 1e-12, "existing default multiplier still applies")
				require.InDelta(t, total*1.1, users.lastAmount, 1e-12)
			})
		}
	}
}

func TestOpenAIUsageBillingPreservesCacheTTLDetails(t *testing.T) {
	for _, tc := range []struct {
		name      string
		usage     OpenAIUsage
		input     int
		aggregate int
	}{
		{"explicit aggregate", OpenAIUsage{InputTokens: 150, CacheReadInputTokens: 10, CacheCreationInputTokens: 100, CacheCreation5mTokens: 20, CacheCreation1hTokens: 30}, 40, 100},
		{"details only", OpenAIUsage{InputTokens: 150, CacheReadInputTokens: 10, CacheCreation5mTokens: 20, CacheCreation1hTokens: 30}, 90, 50},
		{"negative detail is not an aggregate", OpenAIUsage{InputTokens: 150, CacheCreation5mTokens: -20, CacheCreation1hTokens: 30}, 120, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tokens := tc.usage.billingUsageTokens()
			require.Equal(t, tc.input, tokens.InputTokens)
			require.Equal(t, tc.aggregate, tokens.CacheCreationTokens)
			require.Equal(t, tc.usage.CacheCreation5mTokens, tokens.CacheCreation5mTokens)
			require.Equal(t, tc.usage.CacheCreation1hTokens, tokens.CacheCreation1hTokens)
		})
	}
}

func TestOpenAIUsageMergeRetainsTTLBucketsAcrossPartialSnapshots(t *testing.T) {
	usage := OpenAIUsage{InputTokens: 200, CacheReadInputTokens: 10, CacheCreationInputTokens: 100, CacheCreation1hTokens: 80, cacheCreationInputTokensPresent: true}
	mergeOpenAIUsageNonZero(&usage, OpenAIUsage{CacheCreation5mTokens: 20})
	mergeOpenAIUsageNonZero(&usage, OpenAIUsage{OutputTokens: 5})
	mergeOpenAIUsageNonZero(&usage, OpenAIUsage{})
	require.Equal(t, 200, usage.InputTokens)
	require.Equal(t, 100, usage.CacheCreationInputTokens)
	require.Equal(t, 20, usage.CacheCreation5mTokens)
	require.Equal(t, 80, usage.CacheCreation1hTokens)
	require.True(t, usage.cacheCreationInputTokensPresent)
	for _, ttlOnly := range []OpenAIUsage{{CacheCreation5mTokens: 1}, {CacheCreation1hTokens: 1}} {
		require.True(t, openAIUsageHasTokens(&ttlOnly))
		require.False(t, openAIResponsesCompletedEventIsEmpty([]byte(`{"type":"response.completed","response":{"output":[]}}`), &ttlOnly))
	}
}

func TestOpenAIUsageExtractionHonorsExplicitZeroCacheWrite(t *testing.T) {
	for _, details := range []string{"input_tokens_details", "prompt_tokens_details"} {
		payload := fmt.Sprintf(`{"usage":{"input_tokens":100,"output_tokens":2,"cache_creation_input_tokens":99,%q:{"cache_write_tokens":0,"cache_creation_tokens":80}}}`, details)
		usage, ok := extractOpenAIUsageFromJSONBytes([]byte(payload))
		require.True(t, ok)
		require.Zero(t, usage.CacheCreationInputTokens, "canonical zero must not fall back to a stale positive alias")
		var streamed OpenAIUsage
		(&OpenAIGatewayService{}).parseSSEUsageBytesWithType([]byte(payload), "response.completed", &streamed)
		require.Zero(t, streamed.CacheCreationInputTokens)
		require.Equal(t, 100, streamed.InputTokens)
	}
}

func TestOpenAIAnthropicConversionPrefersCompleteTerminalResponseUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			payload := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_cache\",\"model\":\"gpt-5.4\"}}\n\n" +
				`data: {"type":"response.completed","usage":{"input_tokens":20,"output_tokens":4,"cache_creation_input_tokens":4,"input_tokens_details":{"cached_tokens":5}},"response":{"id":"resp_cache","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2,"cache_creation_input_tokens":1,"input_tokens_details":{"cached_tokens":2}}}}` + "\n\n"
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
			var result *OpenAIForwardResult
			var err error
			if stream {
				result, err = svc.handleAnthropicStreamingResponse(response, c, rawChatCompletionsTestAccount(), "gpt-5.4", "gpt-5.4", "gpt-5.4", time.Now())
			} else {
				result, err = svc.handleAnthropicBufferedStreamingResponse(response, c, rawChatCompletionsTestAccount(), "gpt-5.4", "gpt-5.4", "gpt-5.4", time.Now())
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 10, result.Usage.InputTokens)
			require.Equal(t, 2, result.Usage.OutputTokens)
			require.Equal(t, 1, result.Usage.CacheCreationInputTokens)
			require.Equal(t, 2, result.Usage.CacheReadInputTokens)
			out := gjson.Get(recorder.Body.String(), "usage")
			if stream {
				for _, line := range strings.Split(recorder.Body.String(), "\n") {
					if strings.HasPrefix(line, "data: ") {
						event := gjson.Parse(strings.TrimPrefix(line, "data: "))
						if event.Get("type").String() == "message_delta" {
							out = event.Get("usage")
						}
					}
				}
			}
			require.True(t, out.Exists())
			tokens := result.Usage.billingUsageTokens()
			require.EqualValues(t, tokens.InputTokens, out.Get("input_tokens").Int())
			require.EqualValues(t, tokens.CacheCreationTokens, out.Get("cache_creation_input_tokens").Int())
		})
	}
}
