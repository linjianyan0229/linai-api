package repository

import (
	"context"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/ent/accountgroup"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Public views receive only the resulting sale range, never upstream account
// identities, credentials, raw snapshots, or the configured resale factor.
// Lite/auth lookups deliberately skip this presentation-only batch query.
func (r *groupRepository) hydrateDynamicRateRanges(ctx context.Context, groups []service.Group) {
	ids := make([]int64, 0)
	for i := range groups {
		if groups[i].DynamicRateEnabled {
			ids = append(ids, groups[i].ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	rows, err := r.client.Account.Query().
		Where(account.DeletedAtIsNil(), account.HasAccountGroupsWith(accountgroup.GroupIDIn(ids...))).
		Select(account.FieldID, account.FieldPlatform, account.FieldType, account.FieldStatus, account.FieldExtra).
		WithAccountGroups(func(q *dbent.AccountGroupQuery) { q.Where(accountgroup.GroupIDIn(ids...)) }).All(ctx)
	if err != nil {
		logger.LegacyPrintf("repository.group", "dynamic rate range unavailable: %v", err)
		return
	}
	byGroup := make(map[int64][]*service.Account)
	for _, row := range rows {
		a := &service.Account{ID: row.ID, Platform: row.Platform, Type: row.Type, Status: row.Status, Extra: row.Extra}
		for _, binding := range row.Edges.AccountGroups {
			byGroup[binding.GroupID] = append(byGroup[binding.GroupID], a)
		}
	}
	now := time.Now()
	for i := range groups {
		g := &groups[i]
		if !g.DynamicRateEnabled {
			continue
		}
		availability := service.DynamicRateAvailability(byGroup[g.ID], now)
		if availability.UpstreamMin != nil {
			v := *availability.UpstreamMin * g.DynamicRateFactor
			g.DynamicRateMin = &v
		}
		if availability.UpstreamMax != nil {
			v := *availability.UpstreamMax * g.DynamicRateFactor
			g.DynamicRateMax = &v
		}
	}
}
