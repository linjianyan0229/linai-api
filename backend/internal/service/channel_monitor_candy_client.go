package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type monitorCandyClient struct {
	http         *http.Client
	baseURL      string
	pollInterval time.Duration
}

type candyTask struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Candy  *struct {
		Status     string `json:"status"`
		DurationMs *int64 `json:"duration_ms"`
	} `json:"candy"`
}

type candyRequest struct {
	Benchmark       string `json:"benchmark"`
	BaseURL         string `json:"base_url"`
	APIKey          string `json:"api_key"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	ServiceTier     string `json:"service_tier,omitempty"`
}

func newMonitorCandyClient() *monitorCandyClient {
	return &monitorCandyClient{
		baseURL: "https://manxue.ai", pollInterval: 3 * time.Second,
		http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			// The POST body contains the upstream key: never forward it to a redirect.
			return http.ErrUseLastResponse
		}},
	}
}

var candyTaskID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func makeCandyRequest(m *ChannelMonitor) (candyRequest, error) {
	if strings.TrimSpace(m.PrimaryModel) == "" {
		return candyRequest{}, ErrChannelMonitorMissingPrimaryModel
	}
	if strings.TrimSpace(m.APIKey) == "" {
		return candyRequest{}, ErrChannelMonitorMissingAPIKey
	}
	base := normalizeEndpoint(m.Endpoint)
	endpoint, err := url.Parse(base)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return candyRequest{}, ErrChannelMonitorInvalidEndpoint
	}
	// Use the same path joining as the normal OpenAI probe, including /v1 and
	// custom prefixes. Manxue appends /responses to this base itself.
	request := candyRequest{
		Benchmark: "candy", BaseURL: strings.TrimSuffix(joinURL(base, "/v1/responses"), "/responses"),
		APIKey: m.APIKey, Model: strings.TrimSpace(m.PrimaryModel),
	}
	if m.BodyOverrideMode != MonitorBodyOverrideModeOff {
		effort, _ := m.BodyOverride["reasoning_effort"].(string)
		if reasoning, ok := m.BodyOverride["reasoning"].(map[string]any); ok {
			if value, ok := reasoning["effort"].(string); ok {
				effort = value
			}
		}
		switch effort {
		case "low", "medium", "high", "xhigh", "max", "ultra":
			request.ReasoningEffort = effort
		}
		tier, _ := m.BodyOverride["service_tier"].(string)
		if tier == "priority" || tier == "ultrafast" {
			request.ServiceTier = tier
		}
	}
	return request, nil
}

func (c *monitorCandyClient) Check(ctx context.Context, m *ChannelMonitor, attemptID string) (result MonitorCandyPoint, checkErr error) {
	request, err := makeCandyRequest(m)
	if err != nil {
		return MonitorCandyPoint{}, err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return MonitorCandyPoint{}, err
	}
	task, err := c.request(ctx, http.MethodPost, "/api/v1/tests", body, attemptID)
	if err != nil {
		return MonitorCandyPoint{}, err
	}
	if !candyTaskID.MatchString(task.ID) {
		return MonitorCandyPoint{}, fmt.Errorf("candy API returned an invalid task ID")
	}
	id := task.ID
	finished := false
	defer func() {
		if checkErr == nil || finished {
			return
		}
		// Cancel the task we created when the monitor is disabled/edited or the
		// local timeout expires, so upstream usage does not continue needlessly.
		cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(cancelCtx, http.MethodDelete, c.baseURL+"/api/v1/tests/"+id, nil)
		if err != nil {
			return
		}
		if resp, err := c.http.Do(req); err == nil {
			resp.Body.Close()
		}
	}()
	for {
		switch task.Status {
		case "succeeded":
			finished = true
			if task.Candy == nil || (task.Candy.Status != "passed" && task.Candy.Status != "incorrect") {
				return MonitorCandyPoint{}, fmt.Errorf("candy API returned no conclusive candy result")
			}
			return MonitorCandyPoint{Status: task.Candy.Status, CheckedAt: time.Now().UTC(), DurationMs: task.Candy.DurationMs}, nil
		case "failed", "cancelled":
			finished = true
			return MonitorCandyPoint{}, fmt.Errorf("candy task %s", task.Status)
		case "running", "queued", "pending":
		default:
			return MonitorCandyPoint{}, fmt.Errorf("candy API returned an unknown task state")
		}
		if err := candyWait(ctx, c.pollInterval); err != nil {
			return MonitorCandyPoint{}, err
		}
		task, err = c.request(ctx, http.MethodGet, "/api/v1/tests/"+id, nil, "")
		if err != nil {
			return MonitorCandyPoint{}, err
		}
	}
}

func (c *monitorCandyClient) request(ctx context.Context, method, path string, body []byte, idempotencyKey string) (*candyTask, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			// An ambiguous POST is not resubmitted: it may already have charged
			// the upstream. The persisted ten-minute claim prevents a retry storm.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("candy API request failed")
		}
		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable) && attempt < 3 {
			delay := time.Duration(2<<attempt) * time.Second
			if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds >= 0 {
				delay = time.Duration(seconds) * time.Second
			} else if date, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil && date.After(time.Now()) {
				delay = time.Until(date)
			}
			resp.Body.Close()
			if err := candyWait(ctx, delay); err != nil {
				return nil, err
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, fmt.Errorf("candy API returned HTTP %d", resp.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
		resp.Body.Close()
		if err != nil || len(data) > 2<<20 {
			return nil, fmt.Errorf("candy API response could not be read")
		}
		var task candyTask
		if err := json.Unmarshal(data, &task); err != nil {
			return nil, fmt.Errorf("candy API returned invalid JSON")
		}
		return &task, nil
	}
}

func candyWait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
