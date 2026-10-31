//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAnnouncementEmailUsesRealPublicationSchema(t *testing.T) {
	ctx := context.Background()
	user := mustCreateAffiliateWithdrawUser(t, integrationEntClient, "announcement-mail")
	repo := NewAnnouncementEmailRepository(integrationDB)
	require.NoError(t, repo.SetPreference(ctx, user.ID, true, "zh"))
	announcements := NewAnnouncementRepository(integrationEntClient)
	a := &service.Announcement{Title: "Local integration announcement", Content: "Not actually sent", Status: "active", NotifyMode: "silent"}
	require.NoError(t, announcements.Create(ctx, a))
	t.Cleanup(func() {
		require.NoError(t, announcements.Delete(ctx, a.ID))
		_, err := integrationDB.ExecContext(ctx, `DELETE FROM announcement_email_subscriptions WHERE user_id=$1`, user.ID)
		require.NoError(t, err)
	})
	var eventID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT id FROM announcement_email_events WHERE announcement_id=$1`, a.ID).Scan(&eventID))
	for i := 0; i < 20; i++ {
		more, err := repo.Plan(ctx, 200)
		require.NoError(t, err)
		if !more {
			break
		}
	}
	var deliveries int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM announcement_email_deliveries WHERE event_id=$1 AND user_id=$2`, eventID, user.ID).Scan(&deliveries))
	require.Equal(t, 1, deliveries)
	a.Content = "Updated integration content"
	require.NoError(t, announcements.Update(ctx, a))
	var events int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM announcement_email_events WHERE announcement_id=$1`, a.ID).Scan(&events))
	require.Equal(t, 2, events)
}
