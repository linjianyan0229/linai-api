//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupTimeRateAdminLifecycle(t *testing.T) {
	repo := &groupRepoStubForAdmin{createID: 9}
	svc := &adminServiceImpl{groupRepo: repo}
	periods := testGroupTimePeriods()
	created, err := svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "daily", Platform: PlatformOpenAI, RateMultiplier: 0.8, TimeRatePeriods: periods})
	require.NoError(t, err)
	require.Equal(t, periods, created.TimeRatePeriods)
	repo.getByID = created
	updated, err := svc.UpdateGroup(context.Background(), created.ID, &UpdateGroupInput{Name: "daily renamed"})
	require.NoError(t, err)
	require.Equal(t, periods, updated.TimeRatePeriods)
	duplicate := cloneGroupForDuplicate(updated, "time-rate-test")
	duplicate.TimeRatePeriods[0].RateMultiplier = 9
	require.Equal(t, 1.2, updated.TimeRatePeriods[0].RateMultiplier)
	empty := []GroupTimeRatePeriod{}
	updated, err = svc.UpdateGroup(context.Background(), created.ID, &UpdateGroupInput{TimeRatePeriods: &empty})
	require.NoError(t, err)
	require.Empty(t, updated.TimeRatePeriods)
}
