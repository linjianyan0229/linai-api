//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type candyRepoStub struct {
	claimed    bool
	claims     int
	completed  []MonitorCandyPoint
	persistErr error
	states     map[int64]*MonitorCandyState
}

func (r *candyRepoStub) Claim(context.Context, *ChannelMonitor, string, string, time.Time) (bool, error) {
	r.claims++
	return r.claimed, nil
}
func (r *candyRepoStub) Complete(ctx context.Context, _ int64, _, _ string, p MonitorCandyPoint) error {
	r.persistErr = ctx.Err()
	r.completed = append(r.completed, p)
	return nil
}
func (r *candyRepoStub) ListStates(context.Context, []int64) (map[int64]*MonitorCandyState, error) {
	return r.states, nil
}

type candyCheckFunc func(context.Context, *ChannelMonitor, string) (MonitorCandyPoint, error)

func (f candyCheckFunc) Check(ctx context.Context, m *ChannelMonitor, id string) (MonitorCandyPoint, error) {
	return f(ctx, m, id)
}

func TestCandyRunRequiresOptInAndAnEnabledOpenAIMonitor(t *testing.T) {
	for _, mutate := range []func(*ChannelMonitor){
		func(m *ChannelMonitor) { m.CandyEnabled = false },
		func(m *ChannelMonitor) { m.Enabled = false },
		func(m *ChannelMonitor) { m.Provider = MonitorProviderAnthropic },
		func(m *ChannelMonitor) { m.CheckMode = MonitorCheckModeQuota },
	} {
		m := candyTestMonitor()
		mutate(m)
		repo := &candyRepoStub{claimed: true}
		svc := NewChannelMonitorCandyService(repo)
		svc.checker = candyCheckFunc(func(context.Context, *ChannelMonitor, string) (MonitorCandyPoint, error) {
			t.Fatal("ineligible monitor reached the external checker")
			return MonitorCandyPoint{}, nil
		})
		require.NoError(t, svc.Run(context.Background(), m))
		require.Zero(t, repo.claims)
	}
	require.ErrorIs(t, validateCandyMonitor(&ChannelMonitor{Provider: MonitorProviderAnthropic, CandyEnabled: true}), ErrChannelMonitorCandyUnsupported)
	require.ErrorIs(t, validateCandyMonitor(&ChannelMonitor{Provider: MonitorProviderOpenAI, CheckMode: MonitorCheckModeQuota, CandyEnabled: true}), ErrChannelMonitorCandyUnsupported)
	require.NoError(t, validateCandyMonitor(candyTestMonitor()))
}

func TestCandyRunDoesNotSubmitWhenAnotherInstanceOwnsTheClaim(t *testing.T) {
	repo := &candyRepoStub{claimed: false}
	svc := NewChannelMonitorCandyService(repo)
	svc.checker = candyCheckFunc(func(context.Context, *ChannelMonitor, string) (MonitorCandyPoint, error) {
		t.Fatal("unclaimed check submitted")
		return MonitorCandyPoint{}, nil
	})
	require.NoError(t, svc.Run(context.Background(), candyTestMonitor()))
	require.Equal(t, 1, repo.claims)
	require.Empty(t, repo.completed)
}

func TestCandyRunPersistsFailureEvenWhenTheMonitorContextIsCancelled(t *testing.T) {
	repo := &candyRepoStub{claimed: true}
	svc := NewChannelMonitorCandyService(repo)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.checker = candyCheckFunc(func(checkCtx context.Context, m *ChannelMonitor, id string) (MonitorCandyPoint, error) {
		require.Equal(t, "configured-model", m.PrimaryModel)
		require.Contains(t, id, "linai-candy-")
		deadline, ok := checkCtx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), MonitorCandyTimeout)
		cancel()
		return MonitorCandyPoint{}, context.Canceled
	})
	require.ErrorIs(t, svc.Run(ctx, candyTestMonitor()), context.Canceled)
	require.NoError(t, repo.persistErr)
	require.Len(t, repo.completed, 1)
	require.Equal(t, "error", repo.completed[0].Status)
}

func TestCandyViewsExcludeErrorsFromPassRateAndHideOutdatedModelResults(t *testing.T) {
	m := candyTestMonitor()
	now := time.Now().UTC()
	repo := &candyRepoStub{states: map[int64]*MonitorCandyState{m.ID: {
		MonitorID: m.ID, Fingerprint: candyFingerprint(m), LastStartedAt: now,
		History: []MonitorCandyPoint{{Status: "passed", CheckedAt: now.Add(-20 * time.Minute)}, {Status: "incorrect", CheckedAt: now.Add(-10 * time.Minute)}, {Status: "error", CheckedAt: now}},
	}}}
	svc := NewChannelMonitorCandyService(repo)
	view := svc.Views(context.Background(), []*ChannelMonitor{m})[m.ID]
	require.Equal(t, "error", view.Status)
	require.Equal(t, 50.0, *view.PassRate)
	require.Equal(t, 600, view.IntervalSeconds)
	require.Equal(t, now.Add(10*time.Minute), *view.NextCheckAt)
	m.PrimaryModel = "a-different-model"
	view = svc.Views(context.Background(), []*ChannelMonitor{m})[m.ID]
	require.Equal(t, "pending", view.Status)
	require.Empty(t, view.History)
	require.Nil(t, view.PassRate)
	m.Provider = MonitorProviderGemini
	require.Empty(t, svc.Views(context.Background(), []*ChannelMonitor{m}))
}

func TestCandySchedulingUsesThePersistedStartAndRespectsAnotherWorkersLease(t *testing.T) {
	m := candyTestMonitor()
	m.APIKey = "OLD:test-upstream-secret"
	started := time.Now().UTC().Add(-3 * time.Minute)
	state := &MonitorCandyState{MonitorID: m.ID, Fingerprint: candyFingerprint(m), LastStartedAt: started}
	repo := &candyRepoStub{states: map[int64]*MonitorCandyState{m.ID: state}}
	svc := newQuotaModeService(&quotaModeRepoStub{monitor: m})
	svc.SetCandyService(NewChannelMonitorCandyService(repo))
	next, err := svc.RunCandyCheck(context.Background(), m.ID)
	require.NoError(t, err)
	require.Equal(t, started.Add(10*time.Minute), next, "restarts and queue delays must not skip a ten-minute window")
	lease := started.Add(11 * time.Minute)
	state.RunningUntil = &lease
	next, err = svc.RunCandyCheck(context.Background(), m.ID)
	require.NoError(t, err)
	require.Equal(t, lease, next, "an active task in another process must not be resubmitted")
	require.Empty(t, repo.completed)
}

type candyRunnerStub struct {
	stubMonitorSvc
	candyCalled    chan int64
	candyCancelled chan int64
}

func (s *candyRunnerStub) RunCandyCheck(ctx context.Context, id int64) (time.Time, error) {
	s.candyCalled <- id
	<-ctx.Done()
	s.candyCancelled <- id
	return time.Time{}, ctx.Err()
}

func TestCandyRunnerSchedulesOnlyOptedInOpenAIAndCancelsOnDisable(t *testing.T) {
	svc := &candyRunnerStub{candyCalled: make(chan int64, 3), candyCancelled: make(chan int64, 3)}
	runner := newChannelMonitorRunner(svc, nil)
	runner.Start()
	defer runner.Stop()
	m := candyTestMonitor()
	m.IntervalSeconds = 3600
	runner.Schedule(m)
	other := *m
	other.ID = 9
	other.Provider = MonitorProviderAnthropic
	runner.Schedule(&other)
	select {
	case id := <-svc.candyCalled:
		require.Equal(t, m.ID, id)
	case <-time.After(time.Second):
		t.Fatal("initial candy check was not scheduled")
	}
	runner.Unschedule(m.ID)
	select {
	case id := <-svc.candyCancelled:
		require.Equal(t, m.ID, id)
	case <-time.After(time.Second):
		t.Fatal("disabled monitor did not cancel its candy check")
	}
	select {
	case id := <-svc.candyCalled:
		t.Fatalf("unexpected candy check for %d", id)
	case <-time.After(20 * time.Millisecond):
	}
}
