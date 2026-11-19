//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerSnapshotValuesPreserveRegisteredPlatformsAndAccountRates(t *testing.T) {
	cache := newSchedulerCacheUnit(t)
	for i, platform := range domain.ConcretePlatformIDs() {
		t.Run(platform, func(t *testing.T) {
			rate := 0.375
			account := memoTestAccountOn(platform, int64(9800+i), "platform-account")
			account.RateMultiplier = &rate
			account.Credentials["openai_capabilities"] = []string{"embeddings"}
			account.Extra["openai_passthrough_model_filter"] = true
			bucket := service.SchedulerBucket{GroupID: int64(9800 + i), Platform: platform, Mode: service.SchedulerModeSingle}
			memoPublish(t, cache, bucket, account)
			got, hit, err := cache.GetSnapshotValues(context.Background(), bucket)
			require.NoError(t, err)
			require.True(t, hit)
			require.Len(t, got, 1)
			require.Equal(t, platform, got[0].Platform)
			require.NotNil(t, got[0].RateMultiplier)
			require.Equal(t, rate, *got[0].RateMultiplier)
			require.Equal(t, []any{"embeddings"}, got[0].Credentials["openai_capabilities"])
			require.Equal(t, true, got[0].Extra["openai_passthrough_model_filter"])
			got[0].Name = "caller-local"
			again, hit, err := cache.GetSnapshotValues(context.Background(), bucket)
			require.NoError(t, err)
			require.True(t, hit)
			require.Equal(t, "platform-account", again[0].Name, "callers may mutate top-level fields without corrupting the shared master")
		})
	}
}

func TestSchedulerProjectionPreservesExistingOpenAIEndpointCapabilities(t *testing.T) {
	for _, capability := range []service.OpenAIEndpointCapability{service.OpenAIEndpointCapabilityChatCompletions, service.OpenAIEndpointCapabilityEmbeddings} {
		t.Run(string(capability), func(t *testing.T) {
			account := service.Account{
				ID: 9901, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
				Credentials: map[string]any{"openai_capabilities": []string{string(capability)}},
			}
			payload, err := json.Marshal(buildSchedulerMetadataAccount(account))
			require.NoError(t, err)
			var restored service.Account
			require.NoError(t, json.Unmarshal(payload, &restored))
			for _, candidate := range []service.OpenAIEndpointCapability{service.OpenAIEndpointCapabilityChatCompletions, service.OpenAIEndpointCapabilityEmbeddings} {
				require.Equal(t, account.SupportsOpenAIEndpointCapability(candidate), restored.SupportsOpenAIEndpointCapability(candidate), "projection must neither widen nor remove endpoint admission")
			}
		})
	}
}
