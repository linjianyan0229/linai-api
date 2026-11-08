package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupDynamicRateAuthCacheRoundtrip(t *testing.T) {
	svc := &APIKeyService{}
	key := profitAuthTestAPIKey()
	key.Group.DynamicRateEnabled, key.Group.DynamicRateFactor = true, 2.5
	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	var restored APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &restored))
	materialized, used, err := svc.applyAuthCacheEntry(key.Key, &restored)
	require.NoError(t, err)
	require.True(t, used)
	require.True(t, materialized.Group.DynamicRateEnabled)
	require.Equal(t, 2.5, materialized.Group.DynamicRateFactor)
}
