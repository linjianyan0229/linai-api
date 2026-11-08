//go:build unit || integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func runAnnouncementPinningContract(t *testing.T, db *sql.DB, client *dbent.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	prefix := fmt.Sprintf("pin-contract-%d-", time.Now().UnixNano())
	var userID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash) VALUES($1,'test-only') RETURNING id`, prefix+"user@example.test").Scan(&userID))
	t.Cleanup(func() {
		_, err := db.ExecContext(context.Background(), `DELETE FROM announcements WHERE title LIKE $1`, prefix+"%")
		require.NoError(t, err)
		_, err = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
		require.NoError(t, err)
	})
	repo := NewAnnouncementRepository(client)
	old := &service.Announcement{Title: prefix + "z-old", Content: "Contract notice", Status: "active", NotifyMode: "popup"}
	require.NoError(t, repo.Create(ctx, old))
	require.False(t, old.IsPinned)
	readAt := time.Unix(1700000000, 0).UTC()
	_, err := db.ExecContext(ctx, `INSERT INTO announcement_reads(announcement_id,user_id,read_at) VALUES($1,$2,$3)`, old.ID, userID, readAt)
	require.NoError(t, err)
	eventCount := func() int {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM announcement_email_events WHERE announcement_id=$1`, old.ID).Scan(&n))
		return n
	}
	require.Equal(t, 1, eventCount())
	stale, err := repo.GetByID(ctx, old.ID)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		pinned, err := repo.SetPinned(ctx, old.ID, true, &userID)
		require.NoError(t, err)
		require.True(t, pinned.IsPinned)
		require.Equal(t, &userID, pinned.UpdatedBy)
		require.Equal(t, old.Content, pinned.Content)
		require.Equal(t, 1, eventCount(), "pinning and repeated requests must not resend emails")
	}
	// More than the user query limit: the older pin must still reach the first page.
	_, err = db.ExecContext(ctx, `INSERT INTO announcements(title,content,status) SELECT $1||n,'Content','active' FROM generate_series(1,205) n`, prefix)
	require.NoError(t, err)
	items, page, err := repo.List(ctx, pagination.PaginationParams{Page: 1, PageSize: 1, SortBy: "title", SortOrder: "asc"}, service.AnnouncementListFilters{Search: prefix, Status: "active"})
	require.NoError(t, err)
	require.EqualValues(t, 206, page.Total)
	require.Len(t, items, 1)
	require.Equal(t, old.ID, items[0].ID)
	active, err := repo.ListActive(ctx, time.Now())
	require.NoError(t, err)
	var found bool
	for _, item := range active {
		if item.ID == old.ID {
			found = true
		}
	}
	require.True(t, found, "priority must be applied before the 200-record query limit")
	// A content form loaded before pinning cannot overwrite the pin flag.
	stale.Title = prefix + "z-edited"
	stale.UpdatedBy = &userID
	require.NoError(t, repo.Update(ctx, stale))
	require.True(t, stale.IsPinned)
	require.Equal(t, 2, eventCount(), "a real content edit still generates its normal publication event")
	unpinned, err := repo.SetPinned(ctx, old.ID, false, &userID)
	require.NoError(t, err)
	require.False(t, unpinned.IsPinned)
	require.Equal(t, 2, eventCount(), "unpinning must not send another email")
	var storedRead time.Time
	require.NoError(t, db.QueryRowContext(ctx, `SELECT read_at FROM announcement_reads WHERE announcement_id=$1 AND user_id=$2`, old.ID, userID).Scan(&storedRead))
	require.True(t, readAt.Equal(storedRead))
	draft := &service.Announcement{Title: prefix + "draft", Content: "Not published", Status: "draft", NotifyMode: "silent"}
	require.NoError(t, repo.Create(ctx, draft))
	pinned, err := repo.SetPinned(ctx, draft.ID, true, &userID)
	require.NoError(t, err)
	require.Equal(t, "draft", pinned.Status)
	active, err = repo.ListActive(ctx, time.Now())
	require.NoError(t, err)
	for _, item := range active {
		require.NotEqual(t, draft.ID, item.ID)
	}
	_, err = repo.SetPinned(ctx, 9223372036854775807, true, &userID)
	require.ErrorIs(t, err, service.ErrAnnouncementNotFound)
}
