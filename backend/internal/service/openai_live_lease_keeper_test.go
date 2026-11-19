package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func waitLiveLeaseKeeperSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("live lease keeper did not signal before the deadline")
	}
}

func TestLiveLeaseKeeperRenewsDuringSlowWorkAndStopsForHandoff(t *testing.T) {
	var refreshes atomic.Int32
	refreshed := make(chan struct{}, 8)
	ctx, stop := startLiveLeaseKeeper(context.Background(), time.Millisecond, func(context.Context) error {
		refreshes.Add(1)
		select {
		case refreshed <- struct{}{}:
		default:
		}
		return nil
	})
	t.Cleanup(func() { _ = stop() })

	// The work context stays usable while creation/handshake work spans several
	// renewal intervals; it is independent of the keeper's stop context.
	waitLiveLeaseKeeperSignal(t, refreshed)
	waitLiveLeaseKeeperSignal(t, refreshed)
	require.NoError(t, ctx.Err())
	require.NoError(t, stop())
	require.NoError(t, stop(), "handoff cleanup must be idempotent")
	require.ErrorIs(t, context.Cause(ctx), context.Canceled)
	stoppedAt := refreshes.Load()
	time.Sleep(5 * time.Millisecond)
	require.Equal(t, stoppedAt, refreshes.Load(), "no refresh may run after handoff or release")
}

func TestLiveLeaseKeeperCancelsSlowWorkOnRenewalFailure(t *testing.T) {
	var refreshes atomic.Int32
	ctx, stop := startLiveLeaseKeeper(context.Background(), time.Millisecond, func(context.Context) error {
		refreshes.Add(1)
		return ErrLiveUnavailable
	})
	t.Cleanup(func() { _ = stop() })
	waitLiveLeaseKeeperSignal(t, ctx.Done())
	require.ErrorIs(t, context.Cause(ctx), ErrLiveUnavailable)
	require.ErrorIs(t, stop(), ErrLiveUnavailable)
	require.ErrorIs(t, stop(), ErrLiveUnavailable)
	require.EqualValues(t, 1, refreshes.Load())
}

func TestLiveLeaseKeeperStopCancelsAndWaitsForInflightRenewal(t *testing.T) {
	entered := make(chan struct{})
	returned := make(chan struct{})
	_, stop := startLiveLeaseKeeper(context.Background(), time.Millisecond, func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		close(returned)
		return context.Cause(ctx)
	})
	t.Cleanup(func() { _ = stop() })
	waitLiveLeaseKeeperSignal(t, entered)
	stopped := make(chan struct{})
	stopResult := make(chan error, 1)
	go func() {
		stopResult <- stop()
		close(stopped)
	}()
	waitLiveLeaseKeeperSignal(t, stopped)
	require.NoError(t, <-stopResult, "intentional stop is not a lease failure")
	select {
	case <-returned:
	default:
		t.Fatal("stop returned while renewal was still running")
	}
}

func TestLiveLeaseKeeperPreservesParentCancellationCause(t *testing.T) {
	parent, cancel := context.WithCancelCause(context.Background())
	want := errors.New("client went away")
	ctx, stop := startLiveLeaseKeeper(parent, time.Hour, func(context.Context) error {
		t.Error("canceled work must not renew")
		return nil
	})
	cancel(want)
	waitLiveLeaseKeeperSignal(t, ctx.Done())
	require.ErrorIs(t, stop(), want)
}

type liveLeaseKeeperTestCache struct {
	ConcurrencyCache
	LiveConcurrencyCache
	refreshes atomic.Int32
	releases  atomic.Int32
	available bool
}

func (c *liveLeaseKeeperTestCache) RefreshLiveLease(context.Context, int64, int64, int64, string) (bool, error) {
	c.refreshes.Add(1)
	return c.available, nil
}

func (c *liveLeaseKeeperTestCache) ReleaseLiveLease(context.Context, int64, int64, int64, string) error {
	c.releases.Add(1)
	return nil
}

func TestRefreshLiveObserverLeaseHonorsCurrentControllerAndExpiry(t *testing.T) {
	storeErr := errors.New("store unavailable")
	for _, tc := range []struct {
		name       string
		controller string
		owner      string
		expiresAt  time.Time
		storeErr   error
		available  bool
		wantErr    error
		refreshes  int32
	}{
		{"owned call", LiveControllerObserver, "owner", time.Now().Add(time.Hour), nil, true, nil, 1},
		{"closed call", LiveControllerClosed, "owner", time.Now().Add(time.Hour), nil, true, ErrLiveCallNotFound, 0},
		{"proxy takeover", LiveControllerProxy, "proxy", time.Now().Add(time.Hour), nil, true, ErrLiveControllerChanged, 0},
		{"other observer", LiveControllerObserver, "other", time.Now().Add(time.Hour), nil, true, ErrLiveControllerChanged, 0},
		{"expired call", LiveControllerObserver, "owner", time.Now().Add(-time.Second), nil, true, context.DeadlineExceeded, 0},
		{"unknown ownership", LiveControllerObserver, "owner", time.Now().Add(time.Hour), storeErr, true, storeErr, 0},
		{"lost lease", LiveControllerObserver, "owner", time.Now().Add(time.Hour), nil, false, ErrLiveUnavailable, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := &LiveCallRecord{
				CallHash: "call", AccountID: 1, UserID: 2, APIKeyID: 3, LeaseID: "lease",
				Controller: tc.controller, ControllerOwner: tc.owner, ExpiresAt: tc.expiresAt,
			}
			store := &liveTestStore{}
			require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))
			store.getCallErr = tc.storeErr
			cache := &liveLeaseKeeperTestCache{available: tc.available}
			svc := &OpenAIGatewayService{concurrencyService: NewConcurrencyService(cache)}
			err := svc.refreshLiveObserverLease(context.Background(), store, record, "owner")
			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantErr)
			}
			require.Equal(t, tc.refreshes, cache.refreshes.Load())
		})
	}
}

func TestLiveObserverDialKeeperCancelsOnControllerTakeover(t *testing.T) {
	record := &LiveCallRecord{
		CallHash: "call", AccountID: 1, UserID: 2, APIKeyID: 3, LeaseID: "lease",
		Controller: LiveControllerObserver, ControllerOwner: "owner", ExpiresAt: time.Now().Add(time.Hour),
	}
	store := &liveTestStore{}
	require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))
	cache := &liveLeaseKeeperTestCache{available: true}
	svc := &OpenAIGatewayService{concurrencyService: NewConcurrencyService(cache)}
	ctx, stop := startLiveLeaseKeeper(context.Background(), time.Millisecond, func(ctx context.Context) error {
		return svc.refreshLiveObserverLease(ctx, store, record, "owner")
	})
	t.Cleanup(func() { _ = stop() })
	closed := *record
	closed.Controller = LiveControllerClosed
	require.NoError(t, store.SaveLiveCall(context.Background(), &closed, time.Hour))
	waitLiveLeaseKeeperSignal(t, ctx.Done())
	require.ErrorIs(t, stop(), ErrLiveCallNotFound)
	stoppedAt := cache.refreshes.Load()
	time.Sleep(5 * time.Millisecond)
	require.Equal(t, stoppedAt, cache.refreshes.Load())
}

func TestAbortLiveCallCreationClosesUpstreamWithoutSavedMapping(t *testing.T) {
	account := &Account{
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "test-token", "chatgpt_account_id": "test-account"},
	}
	cipher := newLiveAttestationCipher(&config.Config{JWT: config.JWTConfig{Secret: "lease-cleanup-test"}})
	encrypted, err := cipher.Encrypt(`{"v":1,"s":0,"t":"test"}`)
	require.NoError(t, err)
	record := &LiveCallRecord{
		CallID: "call_cleanup", CallHash: hashLiveCallID("call_cleanup"),
		AccountID: 1, UserID: 2, APIKeyID: 3, LeaseID: "lease", AttestationCiphertext: encrypted,
	}
	upstream := newLiveTestFrameConn()
	cache := &liveLeaseKeeperTestCache{}
	svc := &OpenAIGatewayService{
		accountRepo:               &liveTestAccountRepo{account: account},
		cache:                     &liveTestStore{}, // SaveLiveCall failed before creating a mapping.
		concurrencyService:        NewConcurrencyService(cache),
		openaiWSPassthroughDialer: &liveTestDialer{conn: upstream},
		liveAttestationCipher:     cipher,
	}
	svc.abortLiveCallCreation(record)
	select {
	case frame := <-upstream.writes:
		require.Equal(t, coderws.MessageText, frame.messageType)
		require.JSONEq(t, `{"type":"session.close"}`, string(frame.payload))
	default:
		t.Fatal("failed creation did not close its upstream call")
	}
	require.EqualValues(t, 1, cache.releases.Load(), "missing mapping must not prevent lease release")
}

type liveLeaseKeeperCloseFailureStore struct {
	*liveTestStore
}

func (s *liveLeaseKeeperCloseFailureStore) MarkLiveCallClosed(context.Context, string, time.Duration) (bool, error) {
	return false, errors.New("mapping write unavailable")
}

func TestAbortLiveCallCreationReleasesWhenMappingCleanupFails(t *testing.T) {
	cache := &liveLeaseKeeperTestCache{}
	svc := &OpenAIGatewayService{
		accountRepo:        &liveTestAccountRepo{}, // Upstream cleanup cannot dial this account.
		cache:              &liveLeaseKeeperCloseFailureStore{liveTestStore: &liveTestStore{}},
		concurrencyService: NewConcurrencyService(cache),
	}
	svc.abortLiveCallCreation(&LiveCallRecord{AccountID: 1, UserID: 2, APIKeyID: 3, LeaseID: "lease"})
	require.EqualValues(t, 1, cache.releases.Load())
}
