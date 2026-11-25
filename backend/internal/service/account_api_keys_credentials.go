package service

import (
	"fmt"
	"strings"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	MaxAccountAPIKeys         = 100
	MaxAccountAPIKeyLength    = 8192
	AccountAPIKeysModeAppend  = "append"
	AccountAPIKeysModeReplace = "replace"
)

// AccountAPIKeys returns a stable, deduplicated snapshot of the configured key
// pool. Legacy accounts without a pool continue to use their single api_key.
// It does not select or rotate a key and never modifies credentials.
func AccountAPIKeys(credentials map[string]any) []string {
	if raw, configured := credentials["api_keys"]; configured {
		keys, err := normalizeAccountAPIKeys(raw)
		if err == nil {
			return keys
		}
		return nil
	}
	if legacy, ok := credentials["api_key"].(string); ok {
		key, err := normalizeAccountAPIKey(legacy)
		if err == nil && key != "" {
			return []string{key}
		}
	}
	return nil
}

// NormalizeAccountAPIKeyCredentials applies a credential update without exposing
// existing secrets to callers. api_keys is authoritative when both forms are
// supplied; api_keys_mode is write-only and is never persisted. Missing pool/key
// fields preserve the existing pool, while an explicit single api_key replaces it.
func NormalizeAccountAPIKeyCredentials(accountType string, existing, incoming map[string]any) (map[string]any, error) {
	_, hasPool := incoming["api_keys"]
	_, hasSingle := incoming["api_key"]
	modeValue, hasMode := incoming["api_keys_mode"]
	mode := AccountAPIKeysModeReplace
	if hasMode {
		value, ok := modeValue.(string)
		if !ok || (value != AccountAPIKeysModeAppend && value != AccountAPIKeysModeReplace) {
			return nil, invalidAccountAPIKeys("api_keys_mode must be append or replace")
		}
		mode = value
	}
	if accountType != AccountTypeAPIKey {
		_, hasExistingPool := existing["api_keys"]
		if hasPool || hasMode || hasExistingPool {
			return nil, invalidAccountAPIKeys("api_keys can only be configured on apikey accounts")
		}
		if existing == nil && incoming == nil {
			return nil, nil
		}
		return MergePreservingSensitiveCreds(existing, incoming), nil
	}
	if existing == nil && incoming == nil {
		return nil, nil
	}
	out := MergePreservingSensitiveCreds(existing, incoming)
	delete(out, "api_keys_mode")
	var keys []string
	var err error
	switch {
	case hasPool:
		keys, err = normalizeAccountAPIKeys(incoming["api_keys"])
		if err == nil && mode == AccountAPIKeysModeAppend {
			existingKeys := AccountAPIKeys(existing)
			if raw, exists := existing["api_keys"]; exists {
				existingKeys, err = normalizeAccountAPIKeys(raw)
			}
			if err == nil {
				keys, err = normalizeAccountAPIKeys(append(existingKeys, keys...))
			}
		}
	case hasSingle:
		value, ok := incoming["api_key"].(string)
		if !ok {
			return nil, invalidAccountAPIKeys("api_key must be a string")
		}
		keys, err = normalizeAccountAPIKeys([]string{value})
	default:
		// Preserve the existing snapshot. A general edit must not replace a pool
		// with the legacy compatibility key that is hidden in API responses.
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	_, hadPool := existing["api_keys"]
	if hasPool || hadPool {
		out["api_keys"] = keys
	}
	out["api_key"] = ""
	if len(keys) > 0 {
		out["api_key"] = keys[0]
	}
	return out, nil
}

func normalizeAccountAPIKeys(raw any) ([]string, error) {
	var values []string
	switch value := raw.(type) {
	case []string:
		values = value
	case []any:
		values = make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				return nil, invalidAccountAPIKeys("api_keys must be an array of strings")
			}
			values = append(values, text)
		}
	default:
		return nil, invalidAccountAPIKeys("api_keys must be an array of strings")
	}
	keys := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		key, err := normalizeAccountAPIKey(value)
		if err != nil {
			return nil, err
		}
		if key == "" {
			continue
		}
		if _, found := seen[key]; found {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
		if len(keys) > MaxAccountAPIKeys {
			return nil, invalidAccountAPIKeys(fmt.Sprintf("api_keys may contain at most %d keys", MaxAccountAPIKeys))
		}
	}
	return keys, nil
}

func normalizeAccountAPIKey(value string) (string, error) {
	if strings.ContainsFunc(value, unicode.IsControl) {
		return "", invalidAccountAPIKeys("API keys must not contain control characters")
	}
	value = strings.TrimSpace(value)
	if len(value) > MaxAccountAPIKeyLength {
		return "", invalidAccountAPIKeys(fmt.Sprintf("each API key may contain at most %d bytes", MaxAccountAPIKeyLength))
	}
	return value, nil
}

func invalidAccountAPIKeys(message string) error {
	return infraerrors.BadRequest("INVALID_ACCOUNT_API_KEYS", message)
}
