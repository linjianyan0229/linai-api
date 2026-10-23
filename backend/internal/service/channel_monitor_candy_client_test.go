//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type candyTransport func(*http.Request) (*http.Response, error)

func (f candyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func candyResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func candyTestMonitor() *ChannelMonitor {
	return &ChannelMonitor{ID: 7, Provider: MonitorProviderOpenAI, Endpoint: "https://upstream.example/proxy/v1",
		APIKey: "test-upstream-secret", PrimaryModel: "configured-model", Enabled: true, CandyEnabled: true,
		UpdatedAt: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)}
}

func TestCandyClientSubmitsConfiguredModelAndPollsItsOwnTask(t *testing.T) {
	client := newMonitorCandyClient()
	client.pollInterval = time.Millisecond
	monitor := candyTestMonitor()
	monitor.BodyOverrideMode = MonitorBodyOverrideModeMerge
	monitor.BodyOverride = map[string]any{"reasoning": map[string]any{"effort": "high"}, "service_tier": "priority"}
	gets := 0
	client.http.Transport = candyTransport(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "manxue.ai", req.URL.Host)
		require.NotContains(t, req.URL.String(), monitor.APIKey)
		if req.Method == http.MethodPost {
			require.Equal(t, "/api/v1/tests", req.URL.Path)
			require.Equal(t, "attempt-one", req.Header.Get("Idempotency-Key"))
			var payload candyRequest
			require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
			require.Equal(t, candyRequest{Benchmark: "candy", BaseURL: "https://upstream.example/proxy/v1", APIKey: monitor.APIKey, Model: monitor.PrimaryModel, ReasoningEffort: "high", ServiceTier: "priority"}, payload)
			return candyResponse(202, `{"id":"task-one","status":"running"}`), nil
		}
		require.Equal(t, http.MethodGet, req.Method)
		require.Equal(t, "/api/v1/tests/task-one", req.URL.Path)
		gets++
		if gets == 1 {
			return candyResponse(200, `{"id":"task-one","status":"running"}`), nil
		}
		return candyResponse(200, `{"id":"task-one","status":"succeeded","candy":{"status":"incorrect","duration_ms":42000}}`), nil
	})
	point, err := client.Check(context.Background(), monitor, "attempt-one")
	require.NoError(t, err)
	require.Equal(t, "incorrect", point.Status)
	require.EqualValues(t, 42000, *point.DurationMs)
	require.Equal(t, 2, gets)
}

func TestCandyClientNeverTreatsFailureOrMissingVerdictAsDegradation(t *testing.T) {
	for _, body := range []string{
		`{"id":"task","status":"failed","error":"test-upstream-secret"}`,
		`{"id":"task","status":"succeeded"}`,
		`{"id":"task","status":"succeeded","candy":{"status":"error"}}`,
		`{"id":"../../other","status":"running"}`,
	} {
		client := newMonitorCandyClient()
		client.http.Transport = candyTransport(func(*http.Request) (*http.Response, error) { return candyResponse(200, body), nil })
		point, err := client.Check(context.Background(), candyTestMonitor(), "attempt")
		require.Error(t, err)
		require.Empty(t, point.Status)
		require.NotContains(t, err.Error(), "test-upstream-secret")
	}
}

func TestCandyClientKeepsIdempotencyKeyWhenRateLimited(t *testing.T) {
	client := newMonitorCandyClient()
	calls := 0
	client.http.Transport = candyTransport(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "same-attempt", req.Header.Get("Idempotency-Key"))
		calls++
		if calls == 1 {
			response := candyResponse(429, `{}`)
			response.Header.Set("Retry-After", "0")
			return response, nil
		}
		return candyResponse(202, `{"id":"task","status":"succeeded","candy":{"status":"passed"}}`), nil
	})
	point, err := client.Check(context.Background(), candyTestMonitor(), "same-attempt")
	require.NoError(t, err)
	require.Equal(t, "passed", point.Status)
	require.Equal(t, 2, calls)
}

func TestCandyClientDoesNotForwardCredentialsOnRedirect(t *testing.T) {
	var leaked atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	client := newMonitorCandyClient()
	client.baseURL = origin.URL
	_, err := client.Check(context.Background(), candyTestMonitor(), "attempt")
	require.Error(t, err)
	require.Zero(t, leaked.Load())
}

func TestCandyClientCancelsItsRemoteTaskWhenMonitorContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := newMonitorCandyClient()
	client.pollInterval = time.Millisecond
	deleted := false
	client.http.Transport = candyTransport(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			return candyResponse(202, `{"id":"own-task","status":"running"}`), nil
		}
		require.Equal(t, "/api/v1/tests/own-task", req.URL.Path)
		if req.Method == http.MethodDelete {
			deleted = true
			require.NoError(t, req.Context().Err())
			return candyResponse(200, `{}`), nil
		}
		cancel()
		return nil, ctx.Err()
	})
	_, err := client.Check(ctx, candyTestMonitor(), "attempt")
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, deleted)
}

func TestCandyBaseURLFollowsMonitorPathJoining(t *testing.T) {
	for _, base := range []string{"https://upstream.example", "https://upstream.example/v1", "https://upstream.example/prefix", "https://upstream.example/prefix/v1"} {
		monitor := candyTestMonitor()
		monitor.Endpoint = base
		payload, err := makeCandyRequest(monitor)
		require.NoError(t, err)
		require.Equal(t, joinURL(base, "/v1/responses"), payload.BaseURL+"/responses")
	}
}
