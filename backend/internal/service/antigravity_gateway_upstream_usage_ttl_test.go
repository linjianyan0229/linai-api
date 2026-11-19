package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAntigravityUpstreamUsageTTLPartialSnapshots(t *testing.T) {
	svc := &AntigravityGatewayService{}
	usage := &ClaudeUsage{}
	svc.extractSSEUsage(`data: {"type":"message_start","message":{"usage":{"input_tokens":10,"cache_creation":{"ephemeral_5m_input_tokens":20}}}}`, usage)
	require.Equal(t, 20, usage.CacheCreationInputTokens)
	svc.extractSSEUsage(`data: {"type":"message_delta","usage":{"cache_creation":{"ephemeral_5m_input_tokens":null,"ephemeral_1h_input_tokens":30}}}`, usage)
	require.Equal(t, 50, usage.CacheCreationInputTokens)
	require.Equal(t, 20, usage.CacheCreation5mTokens)
	require.Equal(t, 30, usage.CacheCreation1hTokens)
	svc.extractSSEUsage(`data: {"type":"message_delta","usage":{"cache_creation_input_tokens":80}}`, usage)
	svc.extractSSEUsage(`data: {"type":"message_delta","usage":{"cache_creation_input_tokens":0,"cache_creation":{"ephemeral_1h_input_tokens":200}}}`, usage)
	require.Equal(t, 80, usage.CacheCreationInputTokens, "a positive aggregate remains authoritative over later details")
	require.True(t, usage.cacheCreationInputTokensPresent)
	require.False(t, usage.cacheCreationInputTokensFromDetails)
	require.Equal(t, 10, usage.InputTokens)
}

func TestAntigravityForwardUpstreamPreservesUsageTTL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			responseBody := `{"usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":5,"cache_creation_input_tokens":80,"cache_creation":{"ephemeral_5m_input_tokens":20,"ephemeral_1h_input_tokens":30}}}`
			if stream {
				responseBody = "data: " + `{"type":"message_start","message":{"usage":{"input_tokens":10,"cache_read_input_tokens":5,"cache_creation_input_tokens":80,"cache_creation":{"ephemeral_5m_input_tokens":20}}}}` + "\n\n" +
					"data: " + `{"type":"message_delta","usage":{"output_tokens":2,"cache_creation":{"ephemeral_1h_input_tokens":30}}}` + "\n\n"
			}
			svc := newAntigravityTestService(&config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}})
			svc.httpUpstream = &httpUpstreamStub{resp: &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(responseBody)),
			}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			account := &Account{ID: 71, Name: "upstream", Credentials: map[string]any{"base_url": "https://upstream.example", "api_key": "test-upstream"}}
			body := []byte(fmt.Sprintf(`{"model":"claude-sonnet-4","stream":%t}`, stream))
			result, err := svc.ForwardUpstream(context.Background(), c, account, body)
			require.NoError(t, err)
			require.Equal(t, 10, result.Usage.InputTokens)
			require.Equal(t, 2, result.Usage.OutputTokens)
			require.Equal(t, 5, result.Usage.CacheReadInputTokens)
			require.Equal(t, 80, result.Usage.CacheCreationInputTokens)
			require.Equal(t, 20, result.Usage.CacheCreation5mTokens)
			require.Equal(t, 30, result.Usage.CacheCreation1hTokens)
			require.True(t, result.Usage.cacheCreationInputTokensPresent, "returning ForwardResult must preserve the parser's authority state")
		})
	}
}
