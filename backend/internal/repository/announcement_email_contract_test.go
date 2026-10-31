//go:build unit

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestAnnouncementEmailPostgresContract(t *testing.T) {
	dsn := os.Getenv("LINAI_ANNOUNCEMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("set LINAI_ANNOUNCEMENT_TEST_DSN for isolated announcement delivery tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = admin.Close() }()
	schema := fmt.Sprintf("announcement_mail_%d", time.Now().UnixNano())
	_, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	defer func() {
		_, err := admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
		require.NoError(t, err)
	}()
	connection, err := url.Parse(dsn)
	require.NoError(t, err)
	query := connection.Query()
	query.Set("search_path", schema)
	connection.RawQuery = query.Encode()
	db, err := sql.Open("postgres", connection.String())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	exec := func(query string, args ...any) {
		_, err := db.ExecContext(ctx, query, args...)
		require.NoError(t, err)
	}
	count := func(query string, args ...any) int {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, query, args...).Scan(&n))
		return n
	}
	exec(`CREATE TABLE users(id BIGINT PRIMARY KEY,email TEXT,balance NUMERIC NOT NULL DEFAULT 0,status TEXT NOT NULL DEFAULT 'active',deleted_at TIMESTAMPTZ);
        CREATE TABLE announcements(id BIGSERIAL PRIMARY KEY,title TEXT NOT NULL,content TEXT NOT NULL,status TEXT NOT NULL,
            targeting JSONB,starts_at TIMESTAMPTZ,ends_at TIMESTAMPTZ,notify_mode TEXT DEFAULT 'silent',updated_at TIMESTAMPTZ DEFAULT NOW());
        CREATE TABLE user_subscriptions(user_id BIGINT,group_id BIGINT,status TEXT,expires_at TIMESTAMPTZ,deleted_at TIMESTAMPTZ);
        INSERT INTO users(id,email) VALUES(1,'one@example.test'),(2,'two@example.test'),(3,'three@example.test');
        INSERT INTO announcements(title,content,status) VALUES('Legacy','Existing announcement','active')`)
	migration, err := os.ReadFile("../../migrations/251_announcement_email_push.sql")
	require.NoError(t, err)
	exec(string(migration))
	repo := NewAnnouncementEmailRepository(db)
	p, err := repo.Preference(ctx, 1)
	require.NoError(t, err)
	require.False(t, p.Enabled)
	require.Equal(t, 0, count(`SELECT COUNT(*) FROM announcement_email_events`), "upgrades never replay old announcements")
	require.NoError(t, repo.SetPreference(ctx, 1, true, "zh"))
	require.NoError(t, repo.SetPreference(ctx, 2, true, "en"))
	exec(`INSERT INTO announcements(title,content,status) VALUES('New','Draft content','draft')`)
	require.Equal(t, 0, count(`SELECT COUNT(*) FROM announcement_email_events`))
	exec(`UPDATE announcements SET status='active' WHERE id=2`)
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM announcement_email_events`))
	exec(`UPDATE announcements SET updated_at=clock_timestamp(),notify_mode='popup' WHERE id=2`)
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM announcement_email_events`), "metadata-only saves must not resend")

	var workers sync.WaitGroup
	failures := make(chan error, 20)
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := repo.Plan(ctx, 200)
			if err != nil {
				failures <- err
			}
		}()
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	require.Equal(t, 2, count(`SELECT COUNT(*) FROM announcement_email_deliveries`), "parallel fanout creates one row per subscriber")
	require.NoError(t, repo.SetPreference(ctx, 2, false, "en"))
	exec(`UPDATE users SET email='new-one@example.test' WHERE id=1`)
	task, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, task)
	require.Equal(t, "new-one@example.test", task.Email)
	require.NoError(t, repo.Finish(ctx, task.ID, task.Token, "sent"))
	task, err = repo.Claim(ctx)
	require.NoError(t, err)
	require.Nil(t, task, "turning off cancels queued emails")
	require.NoError(t, repo.SetPreference(ctx, 2, true, "en"))
	task, err = repo.Claim(ctx)
	require.NoError(t, err)
	require.Nil(t, task, "reenabling must not replay the old queue")
	require.NoError(t, repo.SetPreference(ctx, 3, true, "en"))
	exec(`UPDATE announcements SET content='Updated content' WHERE id=2`)
	require.Equal(t, 2, count(`SELECT COUNT(*) FROM announcement_email_events`))
	_, err = repo.Plan(ctx, 200)
	require.NoError(t, err)
	tasks := make(chan int64, 20)
	failures = make(chan error, 20)
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			task, err := repo.Claim(ctx)
			if err != nil {
				failures <- err
			}
			if task != nil {
				tasks <- task.ID
				_ = repo.Finish(ctx, task.ID, task.Token, "sent")
			}
		}()
	}
	workers.Wait()
	close(tasks)
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	unique := map[int64]bool{}
	for id := range tasks {
		require.False(t, unique[id])
		unique[id] = true
	}
	require.Len(t, unique, 3, "each new publication reaches each opted-in user only once")

	exec(`INSERT INTO announcements(title,content,status,starts_at) VALUES('Scheduled','Future','active',NOW()+INTERVAL '1 hour')`)
	more, err := repo.Plan(ctx, 200)
	require.NoError(t, err)
	require.False(t, more, "future publications wait for their start time")
	exec(`INSERT INTO announcements(title,content,status,ends_at) VALUES('Expired','Past','active',NOW()-INTERVAL '1 second')`)
	more, err = repo.Plan(ctx, 200)
	require.NoError(t, err)
	require.False(t, more, "expired content is never mailed")
	// Simulate a future announcement becoming due. A subscriber who enabled
	// mail after creation, but before its scheduled start, should receive it.
	exec(`UPDATE announcements SET starts_at=clock_timestamp()-INTERVAL '1 second' WHERE id=3`)
	exec(`UPDATE announcement_email_events SET published_at=NOW()-INTERVAL '1 hour' WHERE announcement_id=3`)
	exec(`UPDATE announcement_email_subscriptions SET enabled_at=NOW()-INTERVAL '30 seconds' WHERE user_id IN (1,2,3)`)
	_, err = repo.Plan(ctx, 200)
	require.NoError(t, err)
	require.Equal(t, 3, count(`SELECT COUNT(*) FROM announcement_email_deliveries d JOIN announcement_email_events e ON e.id=d.event_id WHERE e.announcement_id=3`))
	exec(`UPDATE announcements SET status='archived' WHERE id=3`)
	task, err = repo.Claim(ctx)
	require.NoError(t, err)
	require.Nil(t, task, "archiving cancels outstanding deliveries")

	require.NoError(t, repo.SetPreference(ctx, 2, false, "en"))
	require.NoError(t, repo.SetPreference(ctx, 3, false, "en"))
	exec(`INSERT INTO announcements(title,content,status) VALUES('Retry','SMTP test','active')`)
	_, err = repo.Plan(ctx, 200)
	require.NoError(t, err)
	for attempt := 1; attempt <= 3; attempt++ {
		task, err = repo.Claim(ctx)
		require.NoError(t, err)
		require.NotNil(t, task)
		other, err := repo.Claim(ctx)
		require.NoError(t, err)
		require.Nil(t, other, "a live lease suppresses competing sends")
		require.NoError(t, repo.Finish(ctx, task.ID, "stale-token", "sent"))
		require.Equal(t, 0, count(`SELECT COUNT(*) FROM announcement_email_deliveries WHERE id=$1 AND status='sent'`, task.ID))
		require.NoError(t, repo.Finish(ctx, task.ID, task.Token, "pending"))
		exec(`UPDATE announcement_email_deliveries SET next_attempt_at=NOW()-INTERVAL '1 second' WHERE id=$1`, task.ID)
	}
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM announcement_email_deliveries WHERE status='failed'`))
	task, err = repo.Claim(ctx)
	require.NoError(t, err)
	require.Nil(t, task)

	// Publication transaction rollback also rolls back its queued event.
	before := count(`SELECT COUNT(*) FROM announcement_email_events`)
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO announcements(title,content,status) VALUES('Rolled back','Never send','active')`)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	require.Equal(t, before, count(`SELECT COUNT(*) FROM announcement_email_events`))
}
