package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type announcementEmailRepository struct{ db *sql.DB }

func NewAnnouncementEmailRepository(db *sql.DB) service.AnnouncementEmailRepository {
	return &announcementEmailRepository{db: db}
}

func (r *announcementEmailRepository) Preference(ctx context.Context, userID int64) (*service.AnnouncementEmailPreference, error) {
	p := new(service.AnnouncementEmailPreference)
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(p.enabled,FALSE),u.email
        FROM users u LEFT JOIN announcement_email_subscriptions p ON p.user_id=u.id
        WHERE u.id=$1 AND u.deleted_at IS NULL`, userID).Scan(&p.Enabled, &p.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUserNotFound
	}
	return p, err
}

func (r *announcementEmailRepository) SetPreference(ctx context.Context, userID int64, enabled bool, locale string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO announcement_email_subscriptions(user_id,enabled,locale)
        VALUES($1,$2,$3) ON CONFLICT(user_id) DO UPDATE SET
        enabled=EXCLUDED.enabled,locale=EXCLUDED.locale,updated_at=clock_timestamp(),
        enabled_at=CASE WHEN EXCLUDED.enabled AND NOT announcement_email_subscriptions.enabled
            THEN clock_timestamp() ELSE announcement_email_subscriptions.enabled_at END`, userID, enabled, locale)
	return err
}

// Fanout is paginated and locked per event; two instances never advance its
// cursor independently, and (event,user) remains unique across retries.
func (r *announcementEmailRepository) Plan(ctx context.Context, limit int) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var eventID, cursor int64
	err = tx.QueryRowContext(ctx, `SELECT e.id,e.cursor_user_id
        FROM announcement_email_events e JOIN announcements a ON a.id=e.announcement_id
        WHERE NOT e.fanout_done AND a.status='active' AND a.email_revision=e.revision
        AND (a.starts_at IS NULL OR a.starts_at<=clock_timestamp())
        AND (a.ends_at IS NULL OR a.ends_at>clock_timestamp())
        ORDER BY e.id LIMIT 1 FOR UPDATE OF e SKIP LOCKED`).Scan(&eventID, &cursor)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT p.user_id FROM announcement_email_subscriptions p
        JOIN users u ON u.id=p.user_id
        JOIN announcement_email_events e ON e.id=$1
        JOIN announcements a ON a.id=e.announcement_id
        WHERE p.enabled AND p.user_id>$2 AND u.deleted_at IS NULL AND u.status='active'
        AND p.enabled_at<=GREATEST(e.published_at,COALESCE(a.starts_at,e.published_at))
        ORDER BY p.user_id LIMIT $3`, eventID, cursor, limit)
	if err != nil {
		return false, err
	}
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return false, err
		}
		ids = append(ids, id)
		cursor = id
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return false, err
	}
	if len(ids) > 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO announcement_email_deliveries(event_id,user_id)
            SELECT $1,unnest($2::bigint[]) ON CONFLICT(event_id,user_id) DO NOTHING`, eventID, pq.Array(ids))
		if err != nil {
			return false, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE announcement_email_events SET cursor_user_id=$2,fanout_done=$3 WHERE id=$1`, eventID, cursor, len(ids) < limit)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (r *announcementEmailRepository) Claim(ctx context.Context) (*service.AnnouncementEmailTask, error) {
	token := uuid.NewString()
	task := &service.AnnouncementEmailTask{Token: token}
	var targeting []byte
	var groups pq.Int64Array
	err := r.db.QueryRowContext(ctx, `
WITH candidate AS (
 SELECT d.id FROM announcement_email_deliveries d
 JOIN announcement_email_events e ON e.id=d.event_id
 JOIN announcements a ON a.id=e.announcement_id
 JOIN announcement_email_subscriptions p ON p.user_id=d.user_id
 JOIN users u ON u.id=d.user_id
 WHERE d.attempts<3 AND ((d.status='pending' AND d.next_attempt_at<=clock_timestamp())
    OR (d.status='sending' AND d.lease_until<clock_timestamp()))
 AND p.enabled AND u.deleted_at IS NULL AND u.status='active'
 AND a.status='active' AND a.email_revision=e.revision
 AND (a.starts_at IS NULL OR a.starts_at<=clock_timestamp())
 AND (a.ends_at IS NULL OR a.ends_at>clock_timestamp())
 AND p.enabled_at<=GREATEST(e.published_at,COALESCE(a.starts_at,e.published_at))
 ORDER BY d.id LIMIT 1 FOR UPDATE OF d SKIP LOCKED
), claimed AS (
 UPDATE announcement_email_deliveries d SET status='sending',attempts=attempts+1,
    lease_until=clock_timestamp()+INTERVAL '5 minutes',lease_token=$1
 FROM candidate c WHERE d.id=c.id RETURNING d.id,d.event_id,d.user_id
)
SELECT d.id,u.email,p.locale,a.title,a.content,COALESCE(a.targeting,'{}'::jsonb),u.balance::double precision,
 ARRAY(SELECT group_id FROM user_subscriptions s WHERE s.user_id=u.id AND s.status='active'
    AND s.deleted_at IS NULL AND s.expires_at>clock_timestamp())
FROM claimed d JOIN users u ON u.id=d.user_id
JOIN announcement_email_subscriptions p ON p.user_id=d.user_id
JOIN announcement_email_events e ON e.id=d.event_id
JOIN announcements a ON a.id=e.announcement_id`, token).Scan(&task.ID, &task.Email, &task.Locale, &task.Title, &task.Content, &targeting, &task.Balance, &groups)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(targeting, &task.Targeting); err != nil {
		// Invalid stored rules fail closed rather than exposing targeted content.
		_ = r.Finish(ctx, task.ID, token, "skipped")
		return nil, err
	}
	task.GroupIDs = []int64(groups)
	return task, nil
}

func (r *announcementEmailRepository) Finish(ctx context.Context, id int64, token, status string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE announcement_email_deliveries SET
        status=CASE WHEN $3::text='pending' AND attempts>=3 THEN 'failed' ELSE $3::text END,
        sent_at=CASE WHEN $3::text='sent' THEN clock_timestamp() ELSE NULL END,
        next_attempt_at=clock_timestamp()+make_interval(mins=>attempts),lease_until=NULL
        WHERE id=$1 AND lease_token=$2 AND status='sending'`, id, token, status)
	return err
}
