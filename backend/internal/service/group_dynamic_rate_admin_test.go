//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGroupDynamicRateAdminActivation(t *testing.T) {
	now := time.Now().Add(-time.Minute)
	group := &Group{ID: 7, Name: "dynamic", Platform: PlatformOpenAI, DynamicRateFactor: 2, RateMultiplier: .8}
	groups := &dynamicAdminGroupRepo{groupRepoStubForAdmin: &groupRepoStubForAdmin{getByID: group}, ids: []int64{3}}
	accounts := &dynamicAdminAccounts{accounts: []*Account{dynamicTestAccount(t, 3, .08, now)}}
	svc := &adminServiceImpl{groupRepo: groups, accountRepo: accounts}
	group.DynamicRateEnabled = true
	require.NoError(t, svc.validateDynamicRateActivation(context.Background(), group))
	accounts.accounts[0].Extra = nil
	require.Error(t, svc.validateDynamicRateActivation(context.Background(), group))
	copied := cloneGroupForDuplicate(group, "dynamic-copy")
	require.False(t, copied.DynamicRateEnabled)
	require.Equal(t, 2.0, copied.DynamicRateFactor)
	require.True(t, group.DynamicRateEnabled)
}

type dynamicAdminGroupRepo struct {
	*groupRepoStubForAdmin
	ids []int64
}

func (r *dynamicAdminGroupRepo) GetAccountIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	return r.ids, nil
}

type dynamicAdminAccounts struct {
	AccountRepository
	accounts []*Account
}

func (r *dynamicAdminAccounts) GetByIDs(context.Context, []int64) ([]*Account, error) {
	return r.accounts, nil
}

func TestGroupDynamicRateCreateDisabledKeepsFactor(t *testing.T) {
	repo := &groupRepoStubForAdmin{createID: 9}
	svc := &adminServiceImpl{groupRepo: repo}
	group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "prepared", Platform: PlatformOpenAI, RateMultiplier: 1, DynamicRateFactor: 3})
	require.NoError(t, err)
	require.False(t, group.DynamicRateEnabled)
	require.Equal(t, 3.0, group.DynamicRateFactor)
}
