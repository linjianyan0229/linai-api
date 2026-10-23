package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

const MonitorCandyInterval = 10 * time.Minute
const MonitorCandyTimeout = 9 * time.Minute
const MonitorCandyHistoryLimit = 60

var ErrChannelMonitorCandyUnsupported = infraerrors.BadRequest("CHANNEL_MONITOR_CANDY_UNSUPPORTED", "candy checks require an OpenAI probe monitor")

// MonitorCandyPoint never contains credentials, task IDs, answers, or upstream error bodies.
type MonitorCandyPoint struct {
	Status     string    `json:"status"` // passed / incorrect / error
	CheckedAt  time.Time `json:"checked_at"`
	DurationMs *int64    `json:"duration_ms,omitempty"`
}

type MonitorCandyView struct {
	Enabled         bool                `json:"enabled"`
	Model           string              `json:"model"`
	Status          string              `json:"status"` // pending / passed / incorrect / error
	Running         bool                `json:"running"`
	IntervalSeconds int                 `json:"interval_seconds"`
	LastCheckedAt   *time.Time          `json:"last_checked_at,omitempty"`
	NextCheckAt     *time.Time          `json:"next_check_at,omitempty"`
	PassRate        *float64            `json:"pass_rate,omitempty"`
	History         []MonitorCandyPoint `json:"history"`
}

type MonitorCandyState struct {
	MonitorID     int64
	Fingerprint   string
	LastStartedAt time.Time
	RunningUntil  *time.Time
	History       []MonitorCandyPoint
}

type ChannelMonitorCandyRepository interface {
	Claim(ctx context.Context, monitor *ChannelMonitor, fingerprint, attemptID string, now time.Time) (bool, error)
	Complete(ctx context.Context, monitorID int64, fingerprint, attemptID string, point MonitorCandyPoint) error
	ListStates(ctx context.Context, ids []int64) (map[int64]*MonitorCandyState, error)
}

type monitorCandyChecker interface {
	Check(context.Context, *ChannelMonitor, string) (MonitorCandyPoint, error)
}

type ChannelMonitorCandyService struct {
	repo    ChannelMonitorCandyRepository
	checker monitorCandyChecker
}

func NewChannelMonitorCandyService(repo ChannelMonitorCandyRepository) *ChannelMonitorCandyService {
	return &ChannelMonitorCandyService{repo: repo, checker: newMonitorCandyClient()}
}

func candyEligible(m *ChannelMonitor) bool {
	return m != nil && m.Provider == MonitorProviderOpenAI && defaultCheckMode(m.CheckMode) != MonitorCheckModeQuota
}

func validateCandyMonitor(m *ChannelMonitor) error {
	if m.CandyEnabled && !candyEligible(m) {
		return ErrChannelMonitorCandyUnsupported
	}
	return nil
}

// UpdatedAt changes on credential/model/config edits. Old results cannot label a
// new model or a rekeyed endpoint, and a stale worker cannot overwrite a new claim.
func candyFingerprint(m *ChannelMonitor) string {
	input := fmt.Sprintf("%d|%s|%s|%s|%s|%s", m.ID, m.Provider, m.Endpoint, m.PrimaryModel, m.APIMode, m.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return fmt.Sprintf("%x", sha256.Sum256([]byte(input)))
}

func (s *ChannelMonitorCandyService) Run(ctx context.Context, m *ChannelMonitor) error {
	if s == nil || s.repo == nil || !candyEligible(m) || !m.Enabled || !m.CandyEnabled {
		return nil
	}
	if m.APIKeyDecryptFailed || strings.TrimSpace(m.APIKey) == "" {
		return ErrChannelMonitorAPIKeyDecryptFailed
	}
	attemptID := "linai-candy-" + uuid.NewString()
	fingerprint := candyFingerprint(m)
	claimed, err := s.repo.Claim(ctx, m, fingerprint, attemptID, time.Now().UTC())
	if err != nil || !claimed {
		return err
	}

	checkCtx, cancel := context.WithTimeout(ctx, MonitorCandyTimeout)
	defer cancel()
	point, checkErr := s.checker.Check(checkCtx, m, attemptID)
	if checkErr != nil {
		point = MonitorCandyPoint{Status: "error", CheckedAt: time.Now().UTC()}
	}
	// Persist cancellation/timeout too, so the UI never calls a failed test normal.
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer persistCancel()
	if err := s.repo.Complete(persistCtx, m.ID, fingerprint, attemptID, point); err != nil {
		return err
	}
	return checkErr
}

func (s *ChannelMonitorCandyService) Views(ctx context.Context, monitors []*ChannelMonitor) map[int64]*MonitorCandyView {
	views := make(map[int64]*MonitorCandyView)
	ids := make([]int64, 0, len(monitors))
	for _, m := range monitors {
		if !candyEligible(m) {
			continue
		}
		view := &MonitorCandyView{Enabled: m.CandyEnabled, Model: m.PrimaryModel, Status: "disabled", IntervalSeconds: int(MonitorCandyInterval.Seconds()), History: []MonitorCandyPoint{}}
		views[m.ID] = view
		if !m.CandyEnabled {
			continue
		}
		view.Status = "pending"
		ids = append(ids, m.ID)
	}
	if len(ids) == 0 || s == nil || s.repo == nil {
		return views
	}
	states, err := s.repo.ListStates(ctx, ids)
	if err != nil {
		slog.Warn("channel_monitor: candy states unavailable", "error", err)
		return views
	}
	for _, m := range monitors {
		view, state := views[m.ID], states[m.ID]
		if view == nil || !view.Enabled || state == nil || state.Fingerprint != candyFingerprint(m) {
			continue
		}
		view.Running = state.RunningUntil != nil && state.RunningUntil.After(time.Now())
		next := state.LastStartedAt.Add(MonitorCandyInterval)
		view.NextCheckAt = &next
		view.History = append(view.History, state.History...)
		if len(view.History) > 0 {
			latest := view.History[len(view.History)-1]
			view.Status, view.LastCheckedAt = latest.Status, &latest.CheckedAt
		}
		passed, judged := 0, 0
		for _, point := range view.History {
			if point.Status == "passed" {
				passed++
				judged++
			}
			if point.Status == "incorrect" {
				judged++
			}
		}
		if judged > 0 {
			rate := float64(passed) * 100 / float64(judged)
			view.PassRate = &rate
		}
	}
	return views
}

func (s *ChannelMonitorService) SetCandyService(candy *ChannelMonitorCandyService) { s.candy = candy }

func (s *ChannelMonitorService) RunCandyCheck(ctx context.Context, id int64) (time.Time, error) {
	if s.candy == nil || s.candy.repo == nil || !s.probeRuntime(ctx).ActiveProbesAllowed() {
		return time.Time{}, nil
	}
	m, err := s.Get(ctx, id)
	if err != nil {
		return time.Time{}, err
	}
	checkErr := s.candy.Run(ctx, m)
	states, err := s.candy.repo.ListStates(ctx, []int64{id})
	if err != nil {
		return time.Time{}, checkErr
	}
	state := states[id]
	if state == nil || state.Fingerprint != candyFingerprint(m) {
		return time.Time{}, checkErr
	}
	next := state.LastStartedAt.Add(MonitorCandyInterval)
	if state.RunningUntil != nil && state.RunningUntil.After(next) {
		next = *state.RunningUntil
	}
	return next, checkErr
}
