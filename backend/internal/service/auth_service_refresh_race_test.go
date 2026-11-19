//go:build unit

package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// barrierRefreshTokenCacheStub 让所有并发请求都先读到旧 token，再统一放行，
// 从而确定性地复现 "Get 之后、消费之前" 的 double-spend 窗口。
type barrierRefreshTokenCacheStub struct {
	*emailBindRefreshTokenCacheStub

	target  int
	barrier chan struct{}
	mu      sync.Mutex
	reads   int
}

func newBarrierRefreshTokenCacheStub(target int) *barrierRefreshTokenCacheStub {
	return &barrierRefreshTokenCacheStub{
		emailBindRefreshTokenCacheStub: newEmailBindRefreshTokenCacheStub(),
		target:                         target,
		barrier:                        make(chan struct{}),
	}
}

func (s *barrierRefreshTokenCacheStub) GetRefreshToken(ctx context.Context, tokenHash string) (*service.RefreshTokenData, error) {
	data, err := s.emailBindRefreshTokenCacheStub.GetRefreshToken(ctx, tokenHash)

	s.mu.Lock()
	s.reads++
	if s.reads == s.target {
		close(s.barrier)
	}
	s.mu.Unlock()
	<-s.barrier
	return data, err
}

// 同一个 refresh token 并发刷新时，只能有一个请求成功兑换，其余必须失败。
func TestAuthServiceRefreshTokenPairConsumesTokenOnce(t *testing.T) {
	ctx := service.WithSessionBinding(context.Background(), &service.SessionBinding{IP: "192.0.2.1", UserAgent: "refresh-race-agent"})
	const workers = 8
	cache := newBarrierRefreshTokenCacheStub(workers)

	user := &service.User{
		ID:           41,
		Email:        "refresh-race@example.com",
		Username:     "refresh-race",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
		TokenVersion: 1,
	}
	cfg := &config.Config{
		JWT: config.JWTConfig{
			Secret:                   "test-refresh-race-secret",
			ExpireHour:               1,
			AccessTokenExpireMinutes: 60,
			RefreshTokenExpireDays:   7,
		},
	}
	svc := service.NewAuthService(nil, newEmailBindUserRepoStub(user), nil, cache, cfg, nil, nil, nil, nil, nil, nil, nil, nil)

	pair, err := svc.GenerateTokenPair(ctx, user, "")
	require.NoError(t, err)

	start := make(chan struct{})
	errs := make(chan error, workers)
	pairs := make(chan *service.TokenPairWithUser, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			rotated, refreshErr := svc.RefreshTokenPair(ctx, pair.RefreshToken)
			errs <- refreshErr
			if rotated != nil {
				pairs <- rotated
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(pairs)

	successes, invalid := 0, 0
	for refreshErr := range errs {
		switch {
		case refreshErr == nil:
			successes++
		case errors.Is(refreshErr, service.ErrRefreshTokenInvalid):
			invalid++
		default:
			t.Fatalf("unexpected refresh error: %v", refreshErr)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, workers-1, invalid)
	rotated := <-pairs
	require.NotNil(t, rotated)
	before, err := svc.ValidateToken(pair.AccessToken)
	require.NoError(t, err)
	after, err := svc.ValidateToken(rotated.AccessToken)
	require.NoError(t, err)
	require.NotEmpty(t, before.SessionID)
	require.NotEmpty(t, before.BindingHash)
	require.Equal(t, before.SessionID, after.SessionID)
	require.Equal(t, before.BindingHash, after.BindingHash)
	require.Equal(t, before.TokenVersion, after.TokenVersion)
	require.NotEqual(t, pair.RefreshToken, rotated.RefreshToken)
	_, err = svc.RefreshTokenPair(ctx, pair.RefreshToken)
	require.ErrorIs(t, err, service.ErrRefreshTokenInvalid)
	_, err = svc.RefreshTokenPair(ctx, rotated.RefreshToken)
	require.NoError(t, err, "a duplicate old refresh must not revoke the successfully rotated session")
}

type rotationFailureCacheStub struct {
	*emailBindRefreshTokenCacheStub
	consumeErr   error
	storeErr     error
	consumeCalls int
	storeCalls   int
}

func (s *rotationFailureCacheStub) ConsumeRefreshToken(ctx context.Context, hash string) (*service.RefreshTokenData, error) {
	s.consumeCalls++
	if s.consumeErr != nil {
		return nil, s.consumeErr
	}
	return s.emailBindRefreshTokenCacheStub.ConsumeRefreshToken(ctx, hash)
}

func (s *rotationFailureCacheStub) StoreRefreshToken(ctx context.Context, hash string, data *service.RefreshTokenData, ttl time.Duration) error {
	s.storeCalls++
	if s.storeErr != nil {
		return s.storeErr
	}
	return s.emailBindRefreshTokenCacheStub.StoreRefreshToken(ctx, hash, data, ttl)
}

func newRotationFailureTestService(cache service.RefreshTokenCache, user *service.User, bind bool) *service.AuthService {
	cfg := &config.Config{JWT: config.JWTConfig{
		Secret: "refresh-rotation-test-secret", AccessTokenExpireMinutes: 60, RefreshTokenExpireDays: 7,
	}}
	var settings *service.SettingService
	if bind {
		settings = service.NewSettingService(&emailBindSettingRepoStub{values: map[string]string{service.SettingKeySessionBindingEnabled: "true"}}, cfg)
	}
	return service.NewAuthService(nil, newEmailBindUserRepoStub(user), nil, cache, cfg, settings, nil, nil, nil, nil, nil, nil, nil)
}

func TestAuthServiceRefreshTokenPairConsumeFailureDoesNotIssueTokens(t *testing.T) {
	for _, tc := range []struct {
		name       string
		consumeErr error
		want       error
	}{
		{"already consumed", service.ErrRefreshTokenNotFound, service.ErrRefreshTokenInvalid},
		{"cache unavailable", errors.New("cache unavailable"), service.ErrServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &rotationFailureCacheStub{emailBindRefreshTokenCacheStub: newEmailBindRefreshTokenCacheStub()}
			user := &service.User{ID: 41, Email: "refresh-failure@example.com", Role: service.RoleUser, Status: service.StatusActive, TokenVersion: 1}
			svc := newRotationFailureTestService(cache, user, false)
			pair, err := svc.GenerateTokenPair(context.Background(), user, "original-family")
			require.NoError(t, err)
			cache.consumeErr = tc.consumeErr
			got, err := svc.RefreshTokenPair(context.Background(), pair.RefreshToken)
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, got)
			require.Equal(t, 1, cache.consumeCalls)
			require.Equal(t, 1, cache.storeCalls, "a failed consume must not create a successor")
		})
	}
}

func TestAuthServiceRefreshTokenPairFailedSuccessorDoesNotRestoreConsumedToken(t *testing.T) {
	cache := &rotationFailureCacheStub{emailBindRefreshTokenCacheStub: newEmailBindRefreshTokenCacheStub()}
	user := &service.User{ID: 41, Email: "refresh-store-failure@example.com", Role: service.RoleUser, Status: service.StatusActive, TokenVersion: 1}
	svc := newRotationFailureTestService(cache, user, false)
	pair, err := svc.GenerateTokenPair(context.Background(), user, "original-family")
	require.NoError(t, err)
	cache.storeErr = errors.New("successor storage failed")
	got, err := svc.RefreshTokenPair(context.Background(), pair.RefreshToken)
	require.ErrorContains(t, err, "successor storage failed")
	require.Nil(t, got)
	require.Equal(t, 1, cache.consumeCalls)
	cache.storeErr = nil
	got, err = svc.RefreshTokenPair(context.Background(), pair.RefreshToken)
	require.ErrorIs(t, err, service.ErrRefreshTokenInvalid)
	require.Nil(t, got)
	require.Equal(t, 1, cache.consumeCalls, "replay must fail during lookup after a successful consume")
}

func TestAuthServiceRefreshTokenPairKeepsBindingCheckBeforeConsume(t *testing.T) {
	for _, tc := range []struct {
		name    string
		binding service.SessionBinding
	}{
		{"IP changed", service.SessionBinding{IP: "192.0.2.2", UserAgent: "first-agent"}},
		{"UA changed", service.SessionBinding{IP: "192.0.2.1", UserAgent: "second-agent"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &rotationFailureCacheStub{emailBindRefreshTokenCacheStub: newEmailBindRefreshTokenCacheStub()}
			user := &service.User{ID: 41, Email: "refresh-binding@example.com", Role: service.RoleUser, Status: service.StatusActive, TokenVersion: 1}
			svc := newRotationFailureTestService(cache, user, true)
			ctx := service.WithSessionBinding(context.Background(), &service.SessionBinding{IP: "192.0.2.1", UserAgent: "first-agent"})
			pair, err := svc.GenerateTokenPair(ctx, user, "original-family")
			require.NoError(t, err)
			changed := service.WithSessionBinding(context.Background(), &tc.binding)
			got, err := svc.RefreshTokenPair(changed, pair.RefreshToken)
			require.ErrorIs(t, err, service.ErrSessionBindingMismatch)
			require.Nil(t, got)
			require.Zero(t, cache.consumeCalls)
			hashes, err := cache.GetFamilyTokenHashes(ctx, "original-family")
			require.NoError(t, err)
			require.Empty(t, hashes, "binding mismatch must still revoke the family")
		})
	}
}

func TestAuthServiceRefreshTokenPairUpgradesLegacyUnboundSession(t *testing.T) {
	cache := &rotationFailureCacheStub{emailBindRefreshTokenCacheStub: newEmailBindRefreshTokenCacheStub()}
	user := &service.User{ID: 41, Email: "refresh-legacy@example.com", Role: service.RoleUser, Status: service.StatusActive, TokenVersion: 1}
	svc := newRotationFailureTestService(cache, user, true)
	pair, err := svc.GenerateTokenPair(context.Background(), user, "original-family")
	require.NoError(t, err)
	binding := &service.SessionBinding{IP: "192.0.2.1", UserAgent: "first-agent"}
	ctx := service.WithSessionBinding(context.Background(), binding)
	got, err := svc.RefreshTokenPair(ctx, pair.RefreshToken)
	require.NoError(t, err)
	require.Equal(t, 1, cache.consumeCalls)
	claims, err := svc.ValidateToken(got.AccessToken)
	require.NoError(t, err)
	require.Equal(t, "original-family", claims.SessionID)
	require.Equal(t, binding.Hash(), claims.BindingHash)
}
