package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyConcurrencyAuthCacheRoundtrip(t *testing.T) {
	svc := &APIKeyService{}
	for _, limit := range []int{0, 3, MaxAPIKeyConcurrency} {
		key := &APIKey{ID: 1, UserID: 2, Concurrency: limit, User: &User{ID: 2, Concurrency: 10}}
		payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), key)})
		require.NoError(t, err)
		var restored APIKeyAuthCacheEntry
		require.NoError(t, json.Unmarshal(payload, &restored))
		got, used, err := svc.applyAuthCacheEntry("test-key", &restored)
		require.NoError(t, err)
		require.True(t, used)
		require.Equal(t, limit, got.Concurrency)
		require.Equal(t, 10, got.User.Concurrency)
	}
}

func TestAPIKeyConcurrencyRejectsLegacyAuthSnapshot(t *testing.T) {
	svc := &APIKeyService{}
	got, used, err := svc.applyAuthCacheEntry("test-key", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{Version: 28},
	})
	require.NoError(t, err)
	require.False(t, used)
	require.Nil(t, got)
}
