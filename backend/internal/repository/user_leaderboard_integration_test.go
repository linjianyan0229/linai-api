//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUserLeaderboardUsesRealUsageSchema(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	logs := newUsageLogRepositoryWithSQL(client, tx)
	repo := &userLeaderboardRepository{db: tx}
	account := mustCreateAccount(t, client, &service.Account{Name: "leaderboard-test-account"})
	model := "leaderboard-" + uuid.NewString()
	first := mustCreateUser(t, client, &service.User{Email: "leaderboard-first@example.test", Username: "First"})
	second := mustCreateUser(t, client, &service.User{Email: "leaderboard-second@example.test", Username: "Second"})
	for _, item := range []struct {
		user   *service.User
		tokens int
		quota  float64
	}{{first, 100, 0.00000001}, {second, 200, 0.25}} {
		key := mustCreateApiKey(t, client, &service.APIKey{UserID: item.user.ID, Key: "sk-" + uuid.NewString(), Name: "leaderboard"})
		_, err := logs.Create(ctx, &service.UsageLog{UserID: item.user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(), Model: "upstream-private", RequestedModel: model, InputTokens: item.tokens, ActualCost: item.quota})
		require.NoError(t, err)
	}
	items, err := repo.Top(ctx, model)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, "Second", items[0].Username)
	require.Equal(t, int64(200), items[0].TotalTokens)
	require.Equal(t, "0.25000000", items[0].TotalQuota)
	require.Equal(t, "0.00000001", items[1].TotalQuota)
}

func TestUserLeaderboardTop20AndModelReranking(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	logs := newUsageLogRepositoryWithSQL(client, tx)
	repo := &userLeaderboardRepository{db: tx}
	account := mustCreateAccount(t, client, &service.Account{Name: "leaderboard-ranking-account"})
	model := "rank-" + uuid.NewString()
	var first *service.User
	var firstKey *service.APIKey
	for i := 1; i <= 25; i++ {
		u := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("rank-%d-%s@example.test", i, uuid.NewString()), Username: fmt.Sprintf("Rank %d", i)})
		key := mustCreateApiKey(t, client, &service.APIKey{UserID: u.ID, Key: "sk-" + uuid.NewString(), Name: "ranking"})
		_, err := logs.Create(ctx, &service.UsageLog{UserID: u.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(), Model: model, RequestedModel: model, InputTokens: i * 100, ActualCost: float64(i) / 10})
		require.NoError(t, err)
		if i == 1 {
			first, firstKey = u, key
		}
	}
	items, err := repo.Top(ctx, model)
	require.NoError(t, err)
	require.Len(t, items, 20)
	require.Equal(t, "Rank 25", items[0].Username)
	require.Equal(t, "Rank 6", items[19].Username)
	_, err = logs.Create(ctx, &service.UsageLog{UserID: first.ID, APIKeyID: firstKey.ID, AccountID: account.ID, RequestID: uuid.NewString(), Model: "private-upstream", RequestedModel: model + "-b", InputTokens: 1000, OutputTokens: 200, CacheCreationTokens: 300, CacheReadTokens: 400, ActualCost: 0.00000001})
	require.NoError(t, err)
	filtered, err := repo.Top(ctx, model+"-b")
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, "Rank 1", filtered[0].Username)
	require.Equal(t, int64(1900), filtered[0].TotalTokens)
	require.Equal(t, "0.00000001", filtered[0].TotalQuota)
	unknown, err := repo.Top(ctx, model+"' OR 1=1 --")
	require.NoError(t, err)
	require.Empty(t, unknown)
}
