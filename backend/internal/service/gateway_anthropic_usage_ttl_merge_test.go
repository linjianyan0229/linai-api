package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func TestMergeAnthropicUsagePreservesCacheCreationBreakdownPresence(t *testing.T) {
	var first apicompat.AnthropicUsage
	require.NoError(t, json.Unmarshal([]byte(`{"input_tokens":10,"cache_creation":{"ephemeral_5m_input_tokens":2,"ephemeral_1h_input_tokens":3}}`), &first))
	var later apicompat.AnthropicUsage
	require.NoError(t, json.Unmarshal([]byte(`{"output_tokens":7,"cache_creation":{"ephemeral_1h_input_tokens":0}}`), &later))

	usage := &ClaudeUsage{}
	mergeAnthropicUsage(usage, first)
	mergeAnthropicUsage(usage, later)

	require.Equal(t, 2, usage.CacheCreation5mTokens)
	require.Zero(t, usage.CacheCreation1hTokens)
	require.Equal(t, 2, usage.CacheCreationInputTokens)
	require.Equal(t, 7, usage.OutputTokens)
}

func TestMergeAnthropicUsageCacheOnlyDeltasReconcilePromptTotal(t *testing.T) {
	usage := &ClaudeUsage{}
	for _, snapshot := range []string{
		`{"input_tokens":100,"prompt_tokens":100,"cache_read_input_tokens":30,"cache_creation_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":20}}`,
		`{"input_tokens":0,"output_tokens":7,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"cache_creation":{"ephemeral_1h_input_tokens":10}}`,
		`{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"cache_creation":{"ephemeral_1h_input_tokens":10}}`,
	} {
		var incoming apicompat.AnthropicUsage
		require.NoError(t, json.Unmarshal([]byte(snapshot), &incoming))
		mergeAnthropicUsage(usage, incoming)
	}
	require.Equal(t, 40, usage.InputTokens)
	require.Equal(t, 30, usage.CacheReadInputTokens)
	require.Equal(t, 30, usage.CacheCreationInputTokens)
	require.Equal(t, 20, usage.CacheCreation5mTokens)
	require.Equal(t, 10, usage.CacheCreation1hTokens)
	require.Equal(t, 7, usage.OutputTokens)
}

func TestMergeAnthropicUsageCacheCreationAuthorityAcrossPartialSnapshots(t *testing.T) {
	for _, test := range []struct {
		name      string
		snapshots []string
		want      int
		want5m    int
		want1h    int
	}{
		{
			name: "positive aggregate survives zero and contradictory details",
			snapshots: []string{
				`{"cache_creation_input_tokens":40,"cache_creation":{"ephemeral_5m_input_tokens":20}}`,
				`{"cache_creation_input_tokens":0,"cache_creation":{"ephemeral_1h_input_tokens":200}}`,
				`{"cache_creation_input_tokens":null,"cache_creation":{"ephemeral_5m_input_tokens":null}}`,
			},
			want: 40, want5m: 20, want1h: 200,
		},
		{
			name: "details accumulate until an authoritative total arrives",
			snapshots: []string{
				`{"cache_creation":{"ephemeral_5m_input_tokens":20}}`,
				`{"cache_creation":{"ephemeral_1h_input_tokens":30}}`,
				`{"cache_creation_input_tokens":40}`,
				`{"cache_creation_input_tokens":0}`,
				`{"cache_creation":{"ephemeral_1h_input_tokens":200}}`,
			},
			want: 40, want5m: 20, want1h: 200,
		},
		{
			name: "explicit detail zero replaces only that bucket",
			snapshots: []string{
				`{"cache_creation":{"ephemeral_5m_input_tokens":20,"ephemeral_1h_input_tokens":30}}`,
				`{"cache_creation":{"ephemeral_5m_input_tokens":0}}`,
				`{"cache_creation":{}}`,
			},
			want: 30, want5m: 0, want1h: 30,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			usage := &ClaudeUsage{InputTokens: 10}
			for _, snapshot := range test.snapshots {
				var incoming apicompat.AnthropicUsage
				require.NoError(t, json.Unmarshal([]byte(snapshot), &incoming))
				mergeAnthropicUsage(usage, incoming)
			}
			require.Equal(t, test.want, usage.CacheCreationInputTokens)
			require.Equal(t, test.want5m, usage.CacheCreation5mTokens)
			require.Equal(t, test.want1h, usage.CacheCreation1hTokens)
			require.Equal(t, 10, usage.InputTokens, "native Anthropic input is already uncached")
		})
	}
}

func TestNormalizeAnthropicUsageBeforeResponsesConversionKeepsCanonicalAggregate(t *testing.T) {
	state := apicompat.NewAnthropicEventToResponsesState()
	usage := ClaudeUsage{
		InputTokens: 10, OutputTokens: 7,
		CacheReadInputTokens: 4, CacheCreationInputTokens: 40,
		CacheCreation5mTokens: 20, CacheCreation1hTokens: 30,
	}
	var event apicompat.AnthropicStreamEvent
	require.NoError(t, json.Unmarshal([]byte(`{"type":"message_delta","usage":{"cache_creation":{"ephemeral_1h_input_tokens":200}}}`), &event))
	syncAnthropicResponsesUsage(state, usage)
	normalizeAnthropicEventUsageForResponses(&event, usage)
	require.NotNil(t, event.Usage)
	require.True(t, event.Usage.CacheCreationInputTokensPresent)
	require.Nil(t, event.Usage.CacheCreation)
	apicompat.AnthropicEventToResponsesEvents(&event, state)
	require.Equal(t, 40, state.CacheCreationInputTokens)
	require.Equal(t, 10, state.InputTokens)
	require.Equal(t, 7, state.OutputTokens)
}
