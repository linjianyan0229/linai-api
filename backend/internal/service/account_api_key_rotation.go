package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

type accountAPIKeyRotationState struct {
	mu          sync.Mutex
	fingerprint [sha256.Size]byte
	next        int
}

// Rotation state belongs to the serving process, not the scheduler's account
// snapshots, which can be reloaded and shared by concurrent requests.
var accountAPIKeyRotations sync.Map // account ID -> *accountAPIKeyRotationState

// selectAccountAPIKeyForRequest pins one key to a private account copy. Nested
// protocol bridges and retries reuse that key; only a new inbound request
// advances the account's round-robin cursor. Single-key and OAuth accounts keep
// their existing behavior. No shared credentials map is mutated.
func selectAccountAPIKeyForRequest(c *gin.Context, account *Account) *Account {
	if account == nil || account.Type != AccountTypeAPIKey || account.apiKeySelected {
		return account
	}
	keys := AccountAPIKeys(account.Credentials)
	if len(keys) < 2 {
		return account
	}
	fingerprint := sha256.Sum256([]byte(strings.Join(keys, "\x00")))
	cacheKey := "upstream_api_key_selection:" + strconv.FormatInt(account.ID, 10) + ":" + hex.EncodeToString(fingerprint[:])
	value, _ := accountAPIKeyRotations.LoadOrStore(account.ID, &accountAPIKeyRotationState{})
	state, ok := value.(*accountAPIKeyRotationState)
	if !ok || state == nil {
		state = &accountAPIKeyRotationState{}
		accountAPIKeyRotations.Store(account.ID, state)
	}
	state.mu.Lock()
	selected := ""
	if c != nil {
		selected = c.GetString(cacheKey)
	}
	if selected == "" {
		if state.fingerprint != fingerprint {
			state.fingerprint = fingerprint
			state.next = 0
		}
		selected = keys[state.next]
		state.next = (state.next + 1) % len(keys)
		if c != nil {
			c.Set(cacheKey, selected)
		}
	}
	state.mu.Unlock()

	copy := *account
	copy.Credentials = make(map[string]any, len(account.Credentials))
	for key, value := range account.Credentials {
		copy.Credentials[key] = value
	}
	copy.Credentials["api_key"] = selected
	copy.apiKeySelected = true
	return &copy
}
