package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPassthroughUsageTTLPartialSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name         string
		snapshots    []string
		wantTotals   []int
		want5m       int
		want1h       int
		wantExplicit bool
	}{
		{
			name:       "merged partial details and explicit zero detail",
			snapshots:  []string{`{"cache_creation":{"ephemeral_5m_input_tokens":20}}`, `{"cache_creation":{"ephemeral_1h_input_tokens":30}}`, `{"cache_creation":{"ephemeral_5m_input_tokens":0}}`},
			wantTotals: []int{20, 50, 30}, want1h: 30,
		},
		{
			name:       "null fields do not clear previous details",
			snapshots:  []string{`{"cache_creation_input_tokens":null,"cache_creation":{"ephemeral_5m_input_tokens":20}}`, `{"cache_creation":{"ephemeral_5m_input_tokens":null,"ephemeral_1h_input_tokens":30}}`, `{"cache_creation_input_tokens":null,"cache_creation":{"ephemeral_1h_input_tokens":null}}`},
			wantTotals: []int{20, 50, 50}, want5m: 20, want1h: 30,
		},
		{
			name:       "aggregate remains authoritative over larger details",
			snapshots:  []string{`{"cache_creation_input_tokens":100,"cache_creation":{"ephemeral_5m_input_tokens":20}}`, `{"cache_creation":{"ephemeral_1h_input_tokens":200}}`, `{"cache_creation_input_tokens":0}`},
			wantTotals: []int{100, 100, 100}, want5m: 20, want1h: 200, wantExplicit: true,
		},
		{
			name:       "aggregate replaces derived value",
			snapshots:  []string{`{"cache_creation":{"ephemeral_5m_input_tokens":20,"ephemeral_1h_input_tokens":30}}`, `{"cache_creation_input_tokens":40}`, `{"output_tokens":7}`},
			wantTotals: []int{50, 40, 40}, want5m: 20, want1h: 30, wantExplicit: true,
		},
		{
			name:       "repeated snapshots remain cumulative",
			snapshots:  []string{`{"cache_creation":{"ephemeral_5m_input_tokens":20,"ephemeral_1h_input_tokens":30}}`, `{"cache_creation":{"ephemeral_5m_input_tokens":20,"ephemeral_1h_input_tokens":30}}`},
			wantTotals: []int{50, 50}, want5m: 20, want1h: 30,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usage := &ClaudeUsage{}
			for i, snapshot := range tc.snapshots {
				event := `{"type":"message_delta","usage":` + snapshot + `}`
				if i == 0 {
					event = `{"type":"message_start","message":{"usage":` + snapshot + `}}`
				}
				parseSSEUsagePassthrough(event, usage)
				require.Equal(t, tc.wantTotals[i], usage.CacheCreationInputTokens)
			}
			require.Equal(t, tc.want5m, usage.CacheCreation5mTokens)
			require.Equal(t, tc.want1h, usage.CacheCreation1hTokens)
			require.Equal(t, tc.wantExplicit, usage.cacheCreationInputTokensPresent)
		})
	}
}

func TestPassthroughUsageReconcilesPromptTotalOnCacheOnlyDeltas(t *testing.T) {
	for _, cacheRead := range []string{`"cache_read_input_tokens":30`, `"cached_tokens":30`, `"prompt_tokens_details":{"cached_tokens":30}`, `"prompt_cache_hit_tokens":30`} {
		t.Run(cacheRead, func(t *testing.T) {
			usage := &ClaudeUsage{}
			parseSSEUsagePassthrough(`{"type":"message_start","message":{"usage":{"input_tokens":100,"prompt_tokens":100,"cache_creation_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":20},`+cacheRead+`}}}`, usage)
			require.Equal(t, 50, usage.InputTokens)
			for i := 0; i < 2; i++ {
				parseSSEUsagePassthrough(`{"type":"message_delta","usage":{"input_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"cache_creation":{"ephemeral_1h_input_tokens":10}}}`, usage)
				require.Equal(t, 40, usage.InputTokens)
				require.Equal(t, 30, usage.CacheReadInputTokens)
				require.Equal(t, 30, usage.CacheCreationInputTokens)
			}
			parseSSEUsagePassthrough(`{"type":"message_delta","usage":{"output_tokens":7}}`, usage)
			require.Equal(t, 40, usage.InputTokens)
			require.Equal(t, 7, usage.OutputTokens)
			require.True(t, usage.promptTotalPresent)
			require.Equal(t, 100, usage.promptTotalTokens)
		})
	}
}

func TestPassthroughUsageExplicitCacheMissOverridesSavedPromptTotal(t *testing.T) {
	usage := &ClaudeUsage{}
	parseSSEUsagePassthrough(`{"type":"message_start","message":{"usage":{"input_tokens":100,"prompt_tokens":100,"cache_read_input_tokens":30,"cache_creation_input_tokens":20}}}`, usage)
	parseSSEUsagePassthrough(`{"type":"message_delta","usage":{"input_tokens":0,"prompt_cache_miss_tokens":0,"prompt_cache_hit_tokens":70,"cache_creation_input_tokens":20}}`, usage)
	require.Zero(t, usage.InputTokens)
	require.Equal(t, 70, usage.CacheReadInputTokens)
	require.False(t, usage.promptTotalPresent, "provider's explicit miss bucket must stop reusing the older prompt total")
	parseSSEUsagePassthrough(`{"type":"message_delta","usage":{"output_tokens":7}}`, usage)
	require.Zero(t, usage.InputTokens)
}

func TestPassthroughUsageNullCacheMissPreservesSavedPromptTotal(t *testing.T) {
	usage := &ClaudeUsage{}
	parseSSEUsagePassthrough(`{"type":"message_start","message":{"usage":{"input_tokens":100,"prompt_tokens":100,"cache_read_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":20}}}}`, usage)
	parseSSEUsagePassthrough(`{"type":"message_delta","usage":{"prompt_cache_miss_tokens":null,"cache_creation":{"ephemeral_1h_input_tokens":10}}}`, usage)
	require.Equal(t, 40, usage.InputTokens)
	require.True(t, usage.promptTotalPresent)
}

func TestPassthroughResponseUsagePreservesTTLAggregatePresence(t *testing.T) {
	for _, tc := range []struct {
		name         string
		creation     string
		want         int
		wantExplicit bool
	}{
		{"missing", ``, 50, false},
		{"null", `"cache_creation_input_tokens":null,`, 50, false},
		{"zero with details", `"cache_creation_input_tokens":0,`, 50, true},
		{"authoritative aggregate", `"cache_creation_input_tokens":80,`, 80, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usage := parseClaudeUsageFromResponseBody([]byte(`{"usage":{"input_tokens":100,"prompt_tokens":100,"cached_tokens":10,` + tc.creation + `"cache_creation":{"ephemeral_5m_input_tokens":20,"ephemeral_1h_input_tokens":30}}}`))
			require.Equal(t, tc.want, usage.CacheCreationInputTokens)
			require.Equal(t, 20, usage.CacheCreation5mTokens)
			require.Equal(t, 30, usage.CacheCreation1hTokens)
			require.Equal(t, tc.wantExplicit, usage.cacheCreationInputTokensPresent)
			require.Equal(t, 100-10-tc.want, usage.InputTokens)
		})
	}
}
