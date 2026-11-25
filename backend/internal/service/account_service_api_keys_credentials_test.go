package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountServiceAPIKeyPoolCreateAndUpdate(t *testing.T) {
	repo := &upstreamBillingProbeAccountRepo{}
	svc := NewAccountService(repo, nil)
	created, err := svc.Create(context.Background(), CreateAccountRequest{
		Name: "pool", Platform: PlatformGemini, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_keys": []any{" first ", "first", "second"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"first", "second"}, AccountAPIKeys(created.Credentials))
	require.Equal(t, "first", created.Credentials["api_key"])
	credentials := map[string]any{"api_keys": []string{"third"}, "api_keys_mode": "append"}
	updated, err := svc.Update(context.Background(), created.ID, UpdateAccountRequest{Credentials: &credentials})
	require.NoError(t, err)
	require.Equal(t, []string{"first", "second", "third"}, AccountAPIKeys(updated.Credentials))
	require.NotContains(t, updated.Credentials, "api_keys_mode")
	credentials = map[string]any{"base_url": "https://upstream.example"}
	updated, err = svc.Update(context.Background(), created.ID, UpdateAccountRequest{Credentials: &credentials})
	require.NoError(t, err)
	require.Equal(t, []string{"first", "second", "third"}, AccountAPIKeys(updated.Credentials))
	credentials = map[string]any{"api_key": "replacement"}
	updated, err = svc.Update(context.Background(), created.ID, UpdateAccountRequest{Credentials: &credentials})
	require.NoError(t, err)
	require.Equal(t, []string{"replacement"}, AccountAPIKeys(updated.Credentials))
}

func TestAccountServiceAPIKeyPoolRejectsInvalidInput(t *testing.T) {
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: {
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "original"},
	}}}
	svc := NewAccountService(repo, nil)
	_, err := svc.Create(context.Background(), CreateAccountRequest{
		Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"api_keys": []string{"key"}},
	})
	require.Error(t, err)
	require.Len(t, repo.accounts, 1)
	invalid := map[string]any{"api_keys": []string{"key\nvalue"}}
	_, err = svc.Update(context.Background(), 1, UpdateAccountRequest{Credentials: &invalid})
	require.Error(t, err)
	require.Equal(t, "original", repo.accounts[1].Credentials["api_key"])
}
