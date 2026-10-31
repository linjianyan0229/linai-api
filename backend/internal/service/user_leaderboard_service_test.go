package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type leaderboardRepoStub struct {
	mu                sync.Mutex
	calls, modelCalls int
	model             string
	err               error
}

func (r *leaderboardRepoStub) Top(_ context.Context, model string) ([]UserLeaderboardEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.model = model
	return []UserLeaderboardEntry{{Username: " Alice ", TotalTokens: 12, TotalQuota: "1.23000000"}}, r.err
}
func (r *leaderboardRepoStub) Models(context.Context) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modelCalls++
	return []string{"gpt-test"}, nil
}

func TestUserLeaderboardCachesSharedResultsWithoutCallerMutation(t *testing.T) {
	repo := new(leaderboardRepoStub)
	svc := NewUserLeaderboardService(repo)
	var workers sync.WaitGroup
	failures := make(chan error, 20)
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := svc.Get(context.Background(), "gpt-test")
			if err != nil {
				failures <- err
			}
		}()
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	require.Equal(t, 1, repo.calls)
	require.Equal(t, 1, repo.modelCalls)
	a, err := svc.Get(context.Background(), "gpt-test")
	require.NoError(t, err)
	a.Items[0].Username = "mutated"
	a.Models[0] = "mutated"
	b, err := svc.Get(context.Background(), "gpt-test")
	require.NoError(t, err)
	require.Equal(t, "Alice", b.Items[0].Username)
	require.Equal(t, "gpt-test", b.Models[0])
	require.Equal(t, 1, b.Items[0].Rank)
	require.Equal(t, 20, b.Limit)
	payload, err := json.Marshal(b)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "email")
	require.NotContains(t, string(payload), "user_id")
	_, err = svc.Get(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, 2, repo.calls)
}

func TestUserLeaderboardValidatesModelAndDoesNotCacheErrors(t *testing.T) {
	repo := &leaderboardRepoStub{err: errors.New("database unavailable")}
	svc := NewUserLeaderboardService(repo)
	_, err := svc.Get(context.Background(), "bad\x00model")
	require.Error(t, err)
	require.Zero(t, repo.calls)
	for i := 0; i < 2; i++ {
		_, err = svc.Get(context.Background(), "gpt-test")
		require.Error(t, err)
	}
	require.Equal(t, 2, repo.calls)
}
