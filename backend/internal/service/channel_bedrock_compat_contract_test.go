//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelBedrockCompatibilityRequiresBoolean(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want bool
	}{
		{"enabled", `{"bedrock_cc_compat":true}`, true},
		{"disabled", `{"bedrock_cc_compat":false}`, false},
		{"absent", `{}`, false},
		{"legacy platform object", `{"bedrock_cc_compat":{"anthropic":true}}`, false},
		{"string is not a boolean", `{"bedrock_cc_compat":"true"}`, false},
		{"null", `{"bedrock_cc_compat":null}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var features map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.raw), &features))
			channel := &Channel{FeaturesConfig: features}
			for _, platform := range []string{PlatformAnthropic, PlatformOpenAI, PlatformGemini} {
				require.Equal(t, tc.want, channel.IsBedrockCCCompatEnabled(platform))
			}
		})
	}
}
