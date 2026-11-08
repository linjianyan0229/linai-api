package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGroupDynamicRateRejectsBeforePaidForward(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkey.Group, &Group{ID: 7, Platform: PlatformOpenAI, DynamicRateEnabled: true, DynamicRateFactor: 2})
	upstream := &httpUpstreamRecorder{}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	for name, call := range map[string]func(*gin.Context) (*OpenAIForwardResult, error){
		"responses": func(c *gin.Context) (*OpenAIForwardResult, error) { return svc.Forward(ctx, c, account, nil) },
		"chat": func(c *gin.Context) (*OpenAIForwardResult, error) {
			return svc.ForwardAsChatCompletions(ctx, c, account, nil, "", "")
		},
		"messages": func(c *gin.Context) (*OpenAIForwardResult, error) {
			return svc.ForwardAsAnthropic(ctx, c, account, nil, "", "")
		},
		"embeddings": func(c *gin.Context) (*OpenAIForwardResult, error) {
			return svc.ForwardEmbeddings(ctx, c, account, nil, "")
		},
		"images": func(c *gin.Context) (*OpenAIForwardResult, error) {
			return svc.ForwardImages(ctx, c, account, nil, nil, "")
		},
		"search": func(c *gin.Context) (*OpenAIForwardResult, error) {
			return svc.ForwardAlphaSearch(ctx, c, account, nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			_, err := call(c)
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.Equal(t, http.StatusServiceUnavailable, failover.StatusCode)
			require.Nil(t, upstream.lastReq)
		})
	}
}

type dynamicChangingBody struct {
	io.Reader
	change func()
}

func (r *dynamicChangingBody) Read(p []byte) (int, error) {
	if r.change != nil {
		r.change()
		r.change = nil
	}
	return r.Reader.Read(p)
}

func TestGroupDynamicRateHTTPResponseCannotRepriceInflightRequest(t *testing.T) {
	now := time.Now().Add(-time.Minute)
	group := &Group{ID: 7, Platform: PlatformOpenAI, DynamicRateEnabled: true, DynamicRateFactor: 2}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)
	account := dynamicTestAccount(t, 3, .08, now)
	account.Credentials = map[string]any{"api_key": "sk-local-fixture", "base_url": "https://api.example.test"}
	responseBody := `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"text-embedding-3-small","usage":{"prompt_tokens":13,"total_tokens":13}}`
	changed := dynamicTestAccount(t, 3, .1, now)
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(&dynamicChangingBody{Reader: strings.NewReader(responseBody), change: func() { account.Extra = changed.Extra }})}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	forward := func() *OpenAIForwardResult {
		body := []byte(`{"model":"text-embedding-3-small","input":"hello"}`)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(body))
		result, err := svc.ForwardEmbeddings(ctx, c, account, body, "")
		require.NoError(t, err)
		require.NotNil(t, result.DynamicGroupPricing)
		return result
	}
	first := forward()
	require.InDelta(t, .16, first.DynamicGroupPricing.Rate, 1e-12)
	upstream.resp.Body = io.NopCloser(strings.NewReader(responseBody))
	second := forward()
	require.InDelta(t, .2, second.DynamicGroupPricing.Rate, 1e-12)
	require.InDelta(t, .16, first.DynamicGroupPricing.Rate, 1e-12)
}
