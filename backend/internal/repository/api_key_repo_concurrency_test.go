package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRepositoryConcurrencyPersistenceAndAuthProjection(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "key-concurrency@example.com")
	key := &service.APIKey{UserID: user.ID, Key: "key-concurrency", Name: "limited", Status: service.StatusActive, Concurrency: 3}
	require.NoError(t, repo.Create(ctx, key))
	for _, read := range []func() (*service.APIKey, error){
		func() (*service.APIKey, error) { return repo.GetByID(ctx, key.ID) },
		func() (*service.APIKey, error) { return repo.GetByKey(ctx, key.Key) },
		func() (*service.APIKey, error) { return repo.GetByKeyForAuth(ctx, key.Key) },
	} {
		got, err := read()
		require.NoError(t, err)
		require.Equal(t, 3, got.Concurrency)
	}

	stale, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	key.Concurrency = 5
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{Concurrency: true}))
	stale.Name = "renamed"
	require.NoError(t, repo.Update(ctx, stale, service.APIKeyUpdateFields{Name: true}))
	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, 5, got.Concurrency, "unrelated edits must not reset the concurrency limit")

	got.Concurrency = 0
	require.NoError(t, repo.Update(ctx, got, service.APIKeyUpdateFields{Concurrency: true}))
	got, err = repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.Zero(t, got.Concurrency)
}

func TestAPIKeyRepositoryConcurrencyDefaultsToZero(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "key-concurrency-default@example.com")
	key, err := client.APIKey.Create().SetUserID(user.ID).SetKey("key-default-concurrency").SetName("default").Save(ctx)
	require.NoError(t, err)
	got, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.Zero(t, got.Concurrency)
}
