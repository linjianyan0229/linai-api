//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupRechargeAdminLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := &groupRepoStubForAdmin{createID: 1}
	svc := &adminServiceImpl{groupRepo: repo}
	tiers := testRechargeTiers()
	created, err := svc.CreateGroup(ctx, &CreateGroupInput{
		Name: "recharge", Platform: PlatformOpenAI, RateMultiplier: 1,
		SubscriptionType: SubscriptionTypeStandard, RechargeRateTiers: tiers,
	})
	require.NoError(t, err)
	require.Equal(t, tiers, created.RechargeRateTiers)
	repo.getByID = created
	updated, err := svc.UpdateGroup(ctx, 1, &UpdateGroupInput{Name: "renamed"})
	require.NoError(t, err)
	require.Equal(t, tiers, updated.RechargeRateTiers, "omitted tiers must preserve existing pricing")
	copy := cloneGroupForDuplicate(updated, "operation")
	copy.RechargeRateTiers[1].MinAmount = 200
	require.Equal(t, float64(100), updated.RechargeRateTiers[1].MinAmount)
	empty := []GroupRechargeRateTier{}
	updated, err = svc.UpdateGroup(ctx, 1, &UpdateGroupInput{RechargeRateTiers: &empty})
	require.NoError(t, err)
	require.Empty(t, updated.RechargeRateTiers)
	updated, err = svc.UpdateGroup(ctx, 1, &UpdateGroupInput{RechargeRateTiers: &tiers})
	require.NoError(t, err)
	require.Len(t, updated.RechargeRateTiers, 3)
	updated, err = svc.UpdateGroup(ctx, 1, &UpdateGroupInput{SubscriptionType: SubscriptionTypeSubscription})
	require.NoError(t, err)
	require.Empty(t, updated.RechargeRateTiers, "converting to a subscription removes balance-recharge levels")
	_, err = svc.UpdateGroup(ctx, 1, &UpdateGroupInput{RechargeRateTiers: &tiers})
	require.Error(t, err)
}

type rechargeProgressRepo struct{ rechargeRateTestRepo }

func (*rechargeProgressRepo) GetByUserID(context.Context, int64) (map[int64]float64, error) {
	return map[int64]float64{1: 0.6}, nil
}

func TestGroupRechargeProgressRespectsGroupAccess(t *testing.T) {
	svc := &APIKeyService{
		userRepo:    &visibilityUserRepo{user: &User{ID: 10, AllowedGroups: []int64{1}, RestrictPublicGroups: true}},
		userSubRepo: &visibilitySubRepo{},
		groupRepo: &visibilityGroupRepo{groups: []Group{
			{ID: 1, Name: "Visible", SubscriptionType: SubscriptionTypeStandard, RechargeRateTiers: testRechargeTiers()},
			{ID: 2, Name: "Restricted", SubscriptionType: SubscriptionTypeStandard, RechargeRateTiers: testRechargeTiers()},
			{ID: 3, Name: "Exclusive", IsExclusive: true, SubscriptionType: SubscriptionTypeStandard, RechargeRateTiers: testRechargeTiers()},
		}},
		userGroupRateRepo: &rechargeProgressRepo{},
	}
	progress, err := svc.GetRechargeProgress(context.Background(), 10, "en")
	require.NoError(t, err)
	require.Equal(t, float64(500), progress.PrincipalUSD)
	require.Len(t, progress.Groups, 1)
	require.Equal(t, "Visible", progress.Groups[0].GroupName)
	require.Equal(t, 3, progress.Groups[0].Level)
	require.Equal(t, 0.6, progress.Groups[0].RateMultiplier)
	require.True(t, progress.Groups[0].HasUserOverride)
}
