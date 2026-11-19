package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type mediaKeyConcurrencyCache struct {
	*helperConcurrencyCacheStub
	keyAllowed      bool
	keyAcquireCalls int
	keyID           int64
	keyLimit        int
}

func (s *mediaKeyConcurrencyCache) AcquireAPIKeySlot(_ context.Context, apiKeyID int64, maxConcurrency int, _ string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keyAcquireCalls++
	s.keyID = apiKeyID
	s.keyLimit = maxConcurrency
	return s.keyAllowed, nil
}

func newMediaKeyConcurrencyContext(path, body string, keyLimit int) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	groupID := int64(3)
	key := &service.APIKey{
		ID: 19, UserID: 7, Concurrency: keyLimit,
		User:    &service.User{ID: 7, Concurrency: 4},
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, Platform: service.PlatformGrok},
	}
	c.Set(string(middleware2.ContextKeyAPIKey), key)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7, Concurrency: 4})
	return c, recorder
}

func TestMediaGatewaysEnforceUserAndAPIKeyConcurrencyBeforeUpstream(t *testing.T) {
	for _, endpoint := range []string{"voice", "realtime", "web_search", "x_search", "batch"} {
		for _, userAllowed := range []bool{false, true} {
			name := endpoint + "/user_full"
			if userAllowed {
				name = endpoint + "/key_full"
			}
			t.Run(name, func(t *testing.T) {
				cache := &mediaKeyConcurrencyCache{
					helperConcurrencyCacheStub: &helperConcurrencyCacheStub{userSeq: []bool{userAllowed}},
				}
				helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
				cfg := &config.Config{RunMode: config.RunModeSimple}
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billing.Stop)
				openAI := &OpenAIGatewayHandler{
					gatewayService:      &service.OpenAIGatewayService{},
					billingCacheService: billing,
					apiKeyService:       &service.APIKeyService{},
					concurrencyHelper:   helper,
				}
				c, recorder := newMediaKeyConcurrencyContext("/v1/"+endpoint, `{"query":"test","input":"test","model":"test","items":[]}`, 2)
				require.NotPanics(t, func() {
					switch endpoint {
					case "voice":
						openAI.GrokVoice(c, "tts")
					case "realtime":
						c.Request.Method = http.MethodGet
						c.Request.Header.Set("Connection", "Upgrade")
						c.Request.Header.Set("Upgrade", "websocket")
						openAI.GrokRealtime(c)
					case "web_search", "x_search":
						h := &GatewayHandler{gatewayService: &service.GatewayService{}, billingCacheService: billing, concurrencyHelper: helper}
						if endpoint == "x_search" {
							h.XSearch(c)
						} else {
							h.WebSearch(c)
						}
					case "batch":
						// A nil submission service proves rejection precedes provider/persistence work.
						(&BatchImageHandler{openAI: openAI}).Submit(c)
					}
				})
				require.Equal(t, http.StatusTooManyRequests, recorder.Code)
				require.NotContains(t, recorder.Body.String(), "data:", "realtime admission must not emit SSE before the WebSocket upgrade")
				require.Equal(t, 0, cache.accountAcquireCalls, "rejected admission must not select or occupy an upstream account")
				require.Equal(t, 1, cache.userAcquireCalls)
				if userAllowed {
					require.Equal(t, 1, cache.keyAcquireCalls)
					require.Equal(t, int64(19), cache.keyID)
					require.Equal(t, 2, cache.keyLimit)
					require.Equal(t, 1, cache.userReleaseCalls, "a full key must return its tentative user slot")
				} else {
					require.Zero(t, cache.keyAcquireCalls, "a key limit must not bypass its user's total limit")
					require.Zero(t, cache.userReleaseCalls)
				}
			})
		}
	}
}

func TestBatchSubmissionReleasesConcurrencyWhenSubmissionFails(t *testing.T) {
	cache := &mediaKeyConcurrencyCache{
		helperConcurrencyCacheStub: &helperConcurrencyCacheStub{userSeq: []bool{true}},
		keyAllowed:                 true,
	}
	openAI := &OpenAIGatewayHandler{
		concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0),
	}
	h := &BatchImageHandler{openAI: openAI, service: &service.BatchImagePublicService{}}
	c, recorder := newMediaKeyConcurrencyContext("/v1/images/batches", `{"model":"test","items":[]}`, 2)
	h.Submit(c)
	require.NotEqual(t, http.StatusOK, recorder.Code, "the unconfigured batch service should reject submission")
	require.Equal(t, 1, cache.keyAcquireCalls)
	require.Equal(t, 1, cache.apiKeyReleaseCalls)
	require.Equal(t, 1, cache.userReleaseCalls)
}

func TestBatchSubmissionLimitedKeyFailsClosedWithoutConcurrencyService(t *testing.T) {
	for _, openAI := range []*OpenAIGatewayHandler{nil, {}} {
		c, recorder := newMediaKeyConcurrencyContext("/v1/images/batches", `{"model":"test","items":[]}`, 2)
		h := &BatchImageHandler{openAI: openAI}
		require.NotPanics(t, func() { h.Submit(c) })
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		require.Contains(t, recorder.Body.String(), "CONCURRENCY_UNAVAILABLE")
	}
}

func TestBatchSubmissionLegacyUnlimitedKeyWithoutGatewayStillReachesService(t *testing.T) {
	c, recorder := newMediaKeyConcurrencyContext("/v1/images/batches", `{"model":"test","items":[]}`, 0)
	h := &BatchImageHandler{service: &service.BatchImagePublicService{}}
	h.Submit(c)
	require.NotEqual(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "CONCURRENCY_UNAVAILABLE")
	require.Contains(t, recorder.Body.String(), "BATCH_IMAGE_DISABLED")
}
