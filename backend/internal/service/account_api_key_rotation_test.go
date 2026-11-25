//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func rotationTestAccount(id int64) *Account {
	return &Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "key-a", "api_keys": []string{"key-a", "key-b", "key-c"}}}
}

func TestAccountAPIKeyRotationRoundRobinAndRequestPinning(t *testing.T) {
	account := rotationTestAccount(910001)
	accountAPIKeyRotations.Delete(account.ID)
	for _, expected := range []string{"key-a", "key-b", "key-c", "key-a"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		selected := selectAccountAPIKeyForRequest(c, account)
		require.Equal(t, expected, selected.GetCredential("api_key"))
		require.NotSame(t, account, selected)
		require.Same(t, selected, selectAccountAPIKeyForRequest(c, selected), "nested bridges keep the selected key")
		require.Equal(t, expected, selectAccountAPIKeyForRequest(c, account).GetCredential("api_key"), "retry with a fresh account snapshot keeps the request key")
		require.Equal(t, "key-a", account.GetCredential("api_key"), "scheduler snapshot is immutable")
	}
}

func TestAccountAPIKeyRotationConcurrentRequests(t *testing.T) {
	account := rotationTestAccount(910002)
	accountAPIKeyRotations.Delete(account.ID)
	var wg sync.WaitGroup
	var mu sync.Mutex
	counts := map[string]int{}
	for range 300 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			selected := selectAccountAPIKeyForRequest(nil, account)
			mu.Lock()
			counts[selected.GetCredential("api_key")]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	require.Equal(t, map[string]int{"key-a": 100, "key-b": 100, "key-c": 100}, counts)
	require.Equal(t, "key-a", account.GetCredential("api_key"))
}

func TestAccountAPIKeyRotationPoolReplacementAndCompatibility(t *testing.T) {
	account := rotationTestAccount(910003)
	accountAPIKeyRotations.Delete(account.ID)
	selectAccountAPIKeyForRequest(nil, account)
	updated := *account
	updated.Credentials = map[string]any{"api_key": "new-a", "api_keys": []string{"new-a", "new-b"}}
	require.Equal(t, "new-a", selectAccountAPIKeyForRequest(nil, &updated).GetCredential("api_key"))
	require.Equal(t, "new-b", selectAccountAPIKeyForRequest(nil, &updated).GetCredential("api_key"))
	single := &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "legacy"}}
	require.Same(t, single, selectAccountAPIKeyForRequest(nil, single))
	oauth := &Account{Type: AccountTypeOAuth, Credentials: account.Credentials}
	require.Same(t, oauth, selectAccountAPIKeyForRequest(nil, oauth))
	require.Nil(t, selectAccountAPIKeyForRequest(nil, nil))
}

func TestAccountAPIKeyRotationResponsesBridgeSendsSelectedAuthorization(t *testing.T) {
	account := forceChatResponsesFallbackAccount()
	account.ID = 910004
	account.Credentials["api_key"] = "key-a"
	account.Credentials["api_keys"] = []string{"key-a", "key-b"}
	accountAPIKeyRotations.Delete(account.ID)
	for _, expected := range []string{"Bearer key-a", "Bearer key-b", "Bearer key-a", "Bearer key-b"} {
		body := []byte(`{"model":"gpt-5.4","input":"hello","stream":false}`)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
		upstream := &httpUpstreamRecorder{resp: &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl_rotation","choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)),
		}}
		svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		_, err := svc.Forward(context.Background(), c, account, body)
		require.NoError(t, err)
		require.Equal(t, expected, upstream.lastReq.Header.Get("Authorization"))
	}
}
