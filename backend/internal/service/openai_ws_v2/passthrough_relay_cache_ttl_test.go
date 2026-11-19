package openai_ws_v2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRelayCacheWriteTTLBucketsSurviveProgressiveAndTurnAccounting(t *testing.T) {
	state := &relayState{}
	message := []byte(`{"type":"response.updated","response":{"usage":{"input_tokens":1100,"output_tokens":10,"input_tokens_details":{"cache_write_tokens":1000,"cache_write_tokens_details":{"ttl_30m_tokens":400,"ttl_12h_tokens":600}}}}}`)
	parseUsageAndAccumulate(state, message, "response.updated", nil)
	require.Equal(t, 400, state.turnUsage.CacheCreation30mInputTokens)
	require.Equal(t, 600, state.turnUsage.CacheCreation12hInputTokens)
	// A missing terminal usage keeps the progressive snapshot for this turn.
	parseUsageAndAccumulate(state, []byte(`{"type":"response.completed","response":{"id":"r"}}`), "response.completed", nil)
	first := finalizeRelayTurnUsage(state)
	require.Equal(t, 1000, first.CacheCreationInputTokens)
	require.Equal(t, 400, first.CacheCreation30mInputTokens)
	require.Equal(t, 600, first.CacheCreation12hInputTokens)
	terminal := []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":500,"output_tokens":0,"input_tokens_details":{"cache_write_tokens":500,"cache_write_tokens_details":{"ttl_30m_tokens":500,"ttl_12h_tokens":0}}}}}`)
	parseUsageAndAccumulate(state, terminal, "response.completed", nil)
	second := finalizeRelayTurnUsage(state)
	require.Equal(t, 500, second.CacheCreation30mInputTokens)
	require.Zero(t, second.CacheCreation12hInputTokens)
	require.Equal(t, 1500, state.usage.CacheCreationInputTokens)
	require.Equal(t, 900, state.usage.CacheCreation30mInputTokens)
	require.Equal(t, 600, state.usage.CacheCreation12hInputTokens)
}

func TestRelayCacheWriteTTLInvalidDetailsKeepAggregate(t *testing.T) {
	for _, details := range []string{`{}`, `{"ttl_12h_tokens":10}`, `{"ttl_30m_tokens":-1,"ttl_12h_tokens":11}`, `{"ttl_30m_tokens":1,"ttl_12h_tokens":10}`} {
		state := &relayState{}
		message := []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":0,"input_tokens_details":{"cache_write_tokens":10,"cache_write_tokens_details":` + details + `}}}}`)
		usage := parseUsageAndAccumulate(state, message, "response.completed", nil)
		require.Equal(t, 10, usage.CacheCreationInputTokens)
		require.Zero(t, usage.CacheCreation30mInputTokens)
		require.Zero(t, usage.CacheCreation12hInputTokens)
	}
}
