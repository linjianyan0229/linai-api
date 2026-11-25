package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeAccountAPIKeyCredentialsPoolUpdates(t *testing.T) {
	legacy := map[string]any{"api_key": "old", "base_url": "https://old.example"}
	pool := map[string]any{"api_key": "first", "api_keys": []string{"first", "second"}}
	cases := []struct {
		name     string
		existing map[string]any
		incoming map[string]any
		want     []string
	}{
		{"new pool", nil, map[string]any{"api_keys": []any{" first ", "", "first", "second"}}, []string{"first", "second"}},
		{"pool overrides compatibility key", nil, map[string]any{"api_keys": []string{"first", "second"}, "api_key": "ignored"}, []string{"first", "second"}},
		{"append legacy", legacy, map[string]any{"api_keys": []string{"new", "old"}, "api_keys_mode": "append"}, []string{"old", "new"}},
		{"append pool", pool, map[string]any{"api_keys": []string{"second", "third"}, "api_keys_mode": "append", "api_key": "second"}, []string{"first", "second", "third"}},
		{"replace default", pool, map[string]any{"api_keys": []string{"third"}}, []string{"third"}},
		{"replace explicit", pool, map[string]any{"api_keys": []string{"third"}, "api_keys_mode": "replace"}, []string{"third"}},
		{"replace with single", pool, map[string]any{"api_key": " new "}, []string{"new"}},
		{"unrelated edit preserves pool", pool, map[string]any{"base_url": "https://new.example"}, []string{"first", "second"}},
		{"unrelated edit preserves legacy", legacy, map[string]any{"base_url": "https://new.example"}, []string{"old"}},
		{"clear pool", pool, map[string]any{"api_keys": []string{}}, []string{}},
		{"clear with single", pool, map[string]any{"api_key": ""}, []string{}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeAccountAPIKeyCredentials(AccountTypeAPIKey, tt.existing, tt.incoming)
			require.NoError(t, err)
			require.Equal(t, tt.want, AccountAPIKeys(got))
			require.NotContains(t, got, "api_keys_mode")
			if len(tt.want) > 0 {
				require.Equal(t, tt.want[0], got["api_key"])
			} else {
				require.Equal(t, "", got["api_key"])
			}
		})
	}
	require.Equal(t, "old", legacy["api_key"])
	require.Equal(t, []string{"first", "second"}, pool["api_keys"])
}

func TestNormalizeAccountAPIKeyCredentialsValidation(t *testing.T) {
	tooMany := make([]string, MaxAccountAPIKeys+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("key-%d", i)
	}
	cases := []struct {
		name string
		kind string
		in   map[string]any
	}{
		{"wrong pool type", AccountTypeAPIKey, map[string]any{"api_keys": "secret"}},
		{"null pool", AccountTypeAPIKey, map[string]any{"api_keys": nil}},
		{"non string item", AccountTypeAPIKey, map[string]any{"api_keys": []any{"key", 1}}},
		{"invalid mode", AccountTypeAPIKey, map[string]any{"api_keys_mode": "random"}},
		{"non string mode", AccountTypeAPIKey, map[string]any{"api_keys_mode": true}},
		{"too many", AccountTypeAPIKey, map[string]any{"api_keys": tooMany}},
		{"oversize key", AccountTypeAPIKey, map[string]any{"api_keys": []string{strings.Repeat("x", MaxAccountAPIKeyLength+1)}}},
		{"control in key", AccountTypeAPIKey, map[string]any{"api_keys": []string{"key\r\nInjected: value"}}},
		{"leading control", AccountTypeAPIKey, map[string]any{"api_key": "\tkey"}},
		{"non string single", AccountTypeAPIKey, map[string]any{"api_key": 5}},
		{"oauth pool", AccountTypeOAuth, map[string]any{"api_keys": []string{"key"}}},
		{"bedrock pool", AccountTypeBedrock, map[string]any{"api_keys": []string{"key"}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeAccountAPIKeyCredentials(tt.kind, nil, tt.in)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "Injected: value")
		})
	}
	_, err := NormalizeAccountAPIKeyCredentials(AccountTypeAPIKey,
		map[string]any{"api_keys": tooMany[:MaxAccountAPIKeys]},
		map[string]any{"api_keys": []string{"one-more"}, "api_keys_mode": "append"})
	require.Error(t, err)
	_, err = NormalizeAccountAPIKeyCredentials(AccountTypeOAuth, map[string]any{"api_keys": []string{}}, nil)
	require.Error(t, err, "changing account type must not retain even an empty API key pool")
}

func TestAccountAPIKeysLegacyAndSnapshot(t *testing.T) {
	require.Equal(t, []string{"legacy"}, AccountAPIKeys(map[string]any{"api_key": " legacy "}))
	require.Empty(t, AccountAPIKeys(map[string]any{"api_key": "legacy", "api_keys": []string{}}))
	require.Empty(t, AccountAPIKeys(map[string]any{"api_key": "legacy", "api_keys": "invalid"}))
	credentials := map[string]any{"api_keys": []string{"first", "second"}}
	keys := AccountAPIKeys(credentials)
	keys[0] = "changed"
	require.Equal(t, []string{"first", "second"}, credentials["api_keys"])
}
