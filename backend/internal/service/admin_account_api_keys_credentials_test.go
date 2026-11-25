//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type accountAPIKeysBulkRepo struct {
	*upstreamBillingProbeAccountRepo
}

func (r *accountAPIKeysBulkRepo) BulkUpdate(_ context.Context, ids []int64, updates AccountBulkUpdate) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bulkUpdates = append(r.bulkUpdates, updates)
	for _, id := range ids {
		if account := r.accounts[id]; account != nil {
			credentials := make(map[string]any, len(account.Credentials)+len(updates.Credentials))
			for key, value := range account.Credentials {
				credentials[key] = value
			}
			for key, value := range updates.Credentials {
				credentials[key] = value
			}
			account.Credentials = credentials
		}
	}
	return int64(len(ids)), nil
}

func TestCreateAccountAPIKeyPoolSupportsProviderPlatforms(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformDeepseek, PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			repo := &upstreamBillingProbeAccountRepo{}
			account, err := (&adminServiceImpl{accountRepo: repo}).CreateAccount(context.Background(), &CreateAccountInput{
				Name: "pool", Platform: platform, Type: AccountTypeAPIKey, SkipDefaultGroupBind: true,
				Credentials: map[string]any{"api_keys": []any{" key-one ", "key-two", "key-one"}, "api_keys_mode": "append"},
			})
			require.NoError(t, err)
			require.Equal(t, []string{"key-one", "key-two"}, AccountAPIKeys(account.Credentials))
			require.Equal(t, "key-one", account.Credentials["api_key"])
			require.NotContains(t, account.Credentials, "api_keys_mode")
		})
	}
}

func TestUpdateAccountAPIKeyPoolAppendAndSingleReplacement(t *testing.T) {
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: {
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "first", "api_keys": []string{"first", "second"}},
	}}}
	svc := &adminServiceImpl{accountRepo: repo}
	updated, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Credentials: map[string]any{
		"api_keys": []string{"third", "second"}, "api_keys_mode": "append", "api_key": "third",
	}})
	require.NoError(t, err)
	require.Equal(t, []string{"first", "second", "third"}, AccountAPIKeys(updated.Credentials))
	require.NotContains(t, updated.Credentials, "api_keys_mode")
	updated, err = svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Credentials: map[string]any{"api_key": "new"}})
	require.NoError(t, err)
	require.Equal(t, []string{"new"}, AccountAPIKeys(updated.Credentials))
}

func TestUpdateAccountAPIKeyPoolRejectsNonAPIKeyTypeChange(t *testing.T) {
	repo := &upstreamBillingProbeAdminRepo{&upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: {
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "first", "api_keys": []string{"first", "second"}},
	}}}}
	_, err := (&adminServiceImpl{accountRepo: repo}).UpdateAccount(context.Background(), 1, &UpdateAccountInput{Type: AccountTypeOAuth})
	require.Error(t, err)
	require.Equal(t, AccountTypeAPIKey, repo.accounts[1].Type)
}

func TestBulkUpdateAccountAPIKeyPoolsAppendToEachExistingPool(t *testing.T) {
	repo := &accountAPIKeysBulkRepo{&upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		1: {ID: 1, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "first", "api_keys": []string{"first", "second"}}},
		2: {ID: 2, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "legacy"}},
	}}}
	result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1, 2}, Credentials: map[string]any{"api_keys": []string{"added"}, "api_keys_mode": "append"},
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.Success)
	require.Equal(t, []string{"first", "second", "added"}, AccountAPIKeys(repo.accounts[1].Credentials))
	require.Equal(t, []string{"legacy", "added"}, AccountAPIKeys(repo.accounts[2].Credentials))
	for _, account := range repo.accounts {
		require.NotContains(t, account.Credentials, "api_keys_mode")
	}
	require.Contains(t, repo.bulkUpdates[0].Extra, UpstreamBillingProbeExtraKey)
}

func TestBulkUpdateAccountSingleAPIKeyReplacesExistingPool(t *testing.T) {
	repo := &accountAPIKeysBulkRepo{&upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		1: {ID: 1, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "first", "api_keys": []string{"first", "second"}}},
	}}}
	_, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1}, Credentials: map[string]any{"api_key": "replacement"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"replacement"}, AccountAPIKeys(repo.accounts[1].Credentials))
}

func TestBulkUpdateAccountAPIKeyPoolsRejectNonAPIKeyBeforeWrite(t *testing.T) {
	repo := &accountAPIKeysBulkRepo{&upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		1: {ID: 1, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "legacy"}},
		2: {ID: 2, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "token"}},
	}}}
	_, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1, 2}, Credentials: map[string]any{"api_keys": []string{"added"}, "api_keys_mode": "append"},
	})
	require.Error(t, err)
	require.Empty(t, repo.bulkUpdates)
	require.Equal(t, []string{"legacy"}, AccountAPIKeys(repo.accounts[1].Credentials))
}

func TestDuplicateAccountAPIKeyPoolCopiesKeysAndRejectsOtherTypes(t *testing.T) {
	for _, kind := range []string{AccountTypeAPIKey, AccountTypeBedrock} {
		t.Run(kind, func(t *testing.T) {
			repo := newDuplicateAccountRepoStub()
			source := &Account{Name: "source", Platform: PlatformAnthropic, Type: kind,
				Credentials: map[string]any{"api_key": "first", "api_keys": []string{"first", "second"}},
			}
			require.NoError(t, repo.Create(context.Background(), source))
			duplicate, err := (&adminServiceImpl{accountRepo: repo, accountDuplicateRepo: repo}).DuplicateAccount(
				context.Background(), source.ID, "", "")
			if kind != AccountTypeAPIKey {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []string{"first", "second"}, AccountAPIKeys(duplicate.Credentials))
			duplicate.Credentials["api_keys"].([]string)[0] = "changed"
			require.Equal(t, []string{"first", "second"}, source.Credentials["api_keys"])
		})
	}
}
