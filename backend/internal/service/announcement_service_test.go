package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type announcementRepoStub struct {
	item   *Announcement
	active []Announcement
}

func (s *announcementRepoStub) Create(_ context.Context, a *Announcement) error {
	s.item = a
	return nil
}

func (s *announcementRepoStub) GetByID(_ context.Context, _ int64) (*Announcement, error) {
	if s.item == nil {
		return nil, ErrAnnouncementNotFound
	}
	return s.item, nil
}

func (s *announcementRepoStub) Update(_ context.Context, a *Announcement) error {
	s.item = a
	return nil
}

func (*announcementRepoStub) Delete(context.Context, int64) error {
	return nil
}

func (*announcementRepoStub) List(context.Context, pagination.PaginationParams, AnnouncementListFilters) ([]Announcement, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (s *announcementRepoStub) SetPinned(ctx context.Context, id int64, pinned bool, actorID *int64) (*Announcement, error) {
	a, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	a.IsPinned, a.UpdatedBy = pinned, actorID
	return a, nil
}

func (s *announcementRepoStub) ListActive(context.Context, time.Time) ([]Announcement, error) {
	return s.active, nil
}

type announcementPinUsers struct{ UserRepository }

func (announcementPinUsers) GetByID(context.Context, int64) (*User, error) {
	return &User{Balance: 5}, nil
}

type announcementPinSubscriptions struct{ UserSubscriptionRepository }

func (announcementPinSubscriptions) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return nil, nil
}

type announcementPinReads struct{ AnnouncementReadRepository }

func (announcementPinReads) GetReadMapByUser(context.Context, int64, []int64) (map[int64]time.Time, error) {
	return map[int64]time.Time{1: time.Unix(1, 0), 3: time.Unix(1, 0)}, nil
}

func TestAnnouncementPinnedPriorityRespectsAudienceAndReads(t *testing.T) {
	future := time.Now().Add(time.Hour)
	repo := &announcementRepoStub{active: []Announcement{
		{ID: 4, Status: AnnouncementStatusActive},
		{ID: 3, Status: AnnouncementStatusActive},
		{ID: 1, Status: AnnouncementStatusActive, IsPinned: true},
		{ID: 8, Status: AnnouncementStatusArchived, IsPinned: true},
		{ID: 9, Status: AnnouncementStatusActive, IsPinned: true, StartsAt: &future},
		{ID: 10, Status: AnnouncementStatusActive, IsPinned: true, Targeting: AnnouncementTargeting{AnyOf: []AnnouncementConditionGroup{
			{AllOf: []AnnouncementCondition{{Type: AnnouncementConditionTypeBalance, Operator: AnnouncementOperatorGT, Value: 100}}},
		}}},
	}}
	svc := NewAnnouncementService(repo, announcementPinReads{}, announcementPinUsers{}, announcementPinSubscriptions{})
	items, err := svc.ListForUser(context.Background(), 1, false)
	require.NoError(t, err)
	require.Len(t, items, 3)
	require.Equal(t, []int64{1, 4, 3}, []int64{items[0].Announcement.ID, items[1].Announcement.ID, items[2].Announcement.ID})
	require.NotNil(t, items[0].ReadAt, "pinning never resets an existing read")
	items, err = svc.ListForUser(context.Background(), 1, true)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int64(4), items[0].Announcement.ID)
}

func TestAnnouncementServiceCreateRejectsEqualStartEndTimes(t *testing.T) {
	repo := &announcementRepoStub{}
	svc := NewAnnouncementService(repo, nil, nil, nil)
	now := time.Unix(1776790020, 0)

	_, err := svc.Create(context.Background(), &CreateAnnouncementInput{
		Title:      "公告",
		Content:    "内容",
		Status:     AnnouncementStatusActive,
		NotifyMode: AnnouncementNotifyModePopup,
		StartsAt:   &now,
		EndsAt:     &now,
	})
	require.ErrorIs(t, err, ErrAnnouncementInvalidSchedule)
}

func TestAnnouncementServiceUpdateRejectsEqualStartEndTimes(t *testing.T) {
	repo := &announcementRepoStub{
		item: &Announcement{
			ID:         1,
			Title:      "公告",
			Content:    "内容",
			Status:     AnnouncementStatusActive,
			NotifyMode: AnnouncementNotifyModePopup,
		},
	}
	svc := NewAnnouncementService(repo, nil, nil, nil)
	now := time.Unix(1776790020, 0)
	startsAt := &now
	endsAt := &now

	_, err := svc.Update(context.Background(), 1, &UpdateAnnouncementInput{
		StartsAt: &startsAt,
		EndsAt:   &endsAt,
	})
	require.ErrorIs(t, err, ErrAnnouncementInvalidSchedule)
}
