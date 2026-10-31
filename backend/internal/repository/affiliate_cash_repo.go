package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

type affiliateCashRepository struct{ *affiliateRepository }

func NewAffiliateCashRepository(client *dbent.Client) service.AffiliateCashRepository {
	return &affiliateCashRepository{&affiliateRepository{client: client}}
}

func lockAffiliateProfile(ctx context.Context, client affiliateQueryExecer, userID int64) error {
	rows, err := client.QueryContext(ctx, `SELECT user_id FROM user_affiliates WHERE user_id=$1 FOR UPDATE`, userID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return rows.Err()
	}
	return rows.Close()
}

// Used by legacy offline withdrawals. The profile lock serializes these with cash
// applications. Oldest available credits are consumed first, in NUMERIC SQL.
func consumeAffiliateCredits(ctx context.Context, client affiliateQueryExecer, userID int64, amount float64) error {
	_, err := client.ExecContext(ctx, `
WITH credits AS (
    SELECT id,remaining_amount,
      COALESCE(SUM(remaining_amount) OVER (ORDER BY created_at,id ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING),0) AS older
    FROM user_affiliate_ledger WHERE user_id=$1 AND action='accrue' AND frozen_until IS NULL AND remaining_amount>0
)
UPDATE user_affiliate_ledger l
SET remaining_amount=l.remaining_amount-LEAST(c.remaining_amount,GREATEST($2::numeric-c.older,0)),updated_at=NOW()
FROM credits c WHERE l.id=c.id`, userID, amount)
	return err
}

func (r *affiliateCashRepository) Summary(ctx context.Context, userID int64) (*service.AffiliateCashSummary, error) {
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, `
SELECT COALESCE(SUM(remaining_amount),0)::text, MIN(created_at+INTERVAL '24 hours'), NOW(),
    (SELECT COALESCE(SUM(amount),0)::text FROM affiliate_cash_applications WHERE user_id=$1 AND status='pending')
FROM user_affiliate_ledger
WHERE user_id=$1 AND action='accrue' AND remaining_amount>0 AND created_at>NOW()-INTERVAL '24 hours' AND created_at<=NOW()`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	summary := new(service.AffiliateCashSummary)
	if !rows.Next() {
		return nil, fmt.Errorf("cash summary missing: %w", rows.Err())
	}
	if err := rows.Scan(&summary.EligibleAmount, &summary.NextExpiresAt, &summary.ServerTime, &summary.PendingAmount); err != nil {
		return nil, err
	}
	return summary, rows.Err()
}

const cashApplicationColumns = `a.id,a.user_id,COALESCE(u.username,''),a.email,a.amount::text,a.currency,a.status,a.payout_not_before,a.created_at,a.resolved_at,a.request_hash`

func scanCashApplication(rows *sql.Rows) (*service.AffiliateCashApplication, error) {
	a := new(service.AffiliateCashApplication)
	err := rows.Scan(&a.ID, &a.UserID, &a.Username, &a.Email, &a.Amount, &a.Currency, &a.Status, &a.PayoutNotBefore, &a.CreatedAt, &a.ResolvedAt, &a.RequestHash)
	return a, err
}

func getCashApplication(ctx context.Context, client affiliateQueryExecer, id, userID int64) (*service.AffiliateCashApplication, error) {
	rows, err := client.QueryContext(ctx, `SELECT `+cashApplicationColumns+` FROM affiliate_cash_applications a JOIN users u ON u.id=a.user_id WHERE a.id=$1 AND ($2::bigint=0 OR a.user_id=$2)`, id, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, service.ErrAffiliateCashNotFound
	}
	return scanCashApplication(rows)
}

func (r *affiliateCashRepository) Create(ctx context.Context, in service.AffiliateCashCreate) (*service.AffiliateCashApplication, error) {
	var result *service.AffiliateCashApplication
	err := r.withTx(ctx, func(ctx context.Context, c *dbent.Client) error {
		if _, err := ensureUserAffiliateWithClient(ctx, c, in.UserID); err != nil {
			return err
		}
		if err := lockAffiliateProfile(ctx, c, in.UserID); err != nil {
			return err
		}
		// Replays are checked before the rolling deadline; a timely accepted
		// request remains valid even when its HTTP retry occurs the next day.
		rows, err := c.QueryContext(ctx, `SELECT id,request_hash FROM affiliate_cash_applications WHERE user_id=$1 AND operation_id=$2`, in.UserID, in.OperationID)
		if err != nil {
			return err
		}
		if rows.Next() {
			var id int64
			var hash string
			err = rows.Scan(&id, &hash)
			_ = rows.Close()
			if err != nil {
				return err
			}
			if hash != in.RequestHash {
				return service.ErrIdempotencyKeyConflict
			}
			result, err = getCashApplication(ctx, c, id, in.UserID)
			if result != nil {
				result.Replayed = true
			}
			return err
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		if _, err = thawFrozenQuotaTx(ctx, c, in.UserID); err != nil {
			return err
		}
		// Capture a fresh DB clock AFTER the profile lock. A request waiting
		// on another transaction cannot reserve credits that expired meanwhile.
		rows, err = c.QueryContext(ctx, `
SELECT id,remaining_amount::text,frozen_until,clock_timestamp()
FROM user_affiliate_ledger WHERE user_id=$1 AND action='accrue' AND remaining_amount>0
AND created_at>clock_timestamp()-INTERVAL '24 hours' AND created_at<=clock_timestamp()
ORDER BY created_at,id FOR UPDATE`, in.UserID)
		if err != nil {
			return err
		}
		type allocation struct {
			id     int64
			amount decimal.Decimal
		}
		var allocations []allocation
		left, err := decimal.NewFromString(in.Amount)
		if err != nil {
			_ = rows.Close()
			return err
		}
		available, frozen := decimal.Zero, decimal.Zero
		notBefore := time.Time{}
		for rows.Next() {
			var id int64
			var value string
			var until *time.Time
			var now time.Time
			if err = rows.Scan(&id, &value, &until, &now); err != nil {
				_ = rows.Close()
				return err
			}
			if notBefore.IsZero() {
				notBefore = now
			}
			if !left.IsPositive() {
				continue
			}
			credit, parseErr := decimal.NewFromString(value)
			if parseErr != nil {
				_ = rows.Close()
				return parseErr
			}
			amount := decimal.Min(credit, left)
			left = left.Sub(amount)
			isFrozen := until != nil // matured credits have been thawed above
			if isFrozen {
				frozen = frozen.Add(amount)
				if until.After(notBefore) {
					notBefore = *until
				}
			} else {
				available = available.Add(amount)
			}
			allocations = append(allocations, allocation{id, amount})
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		if left.IsPositive() || len(allocations) == 0 {
			return service.ErrAffiliateCashUnavailable
		}
		rows, err = c.QueryContext(ctx, `
INSERT INTO affiliate_cash_applications(user_id,email,amount,payment_qr,payment_qr_type,operation_id,request_hash,payout_not_before)
VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, in.UserID, in.Email, in.Amount, in.QR, in.QRType, in.OperationID, in.RequestHash, notBefore)
		if err != nil {
			return err
		}
		var id int64
		if !rows.Next() {
			_ = rows.Close()
			return fmt.Errorf("cash insert returned no id")
		}
		err = rows.Scan(&id)
		_ = rows.Close()
		if err != nil {
			return err
		}
		res, err := c.ExecContext(ctx, `UPDATE user_affiliates SET aff_quota=aff_quota-$2,aff_frozen_quota=aff_frozen_quota-$3,updated_at=NOW() WHERE user_id=$1 AND aff_quota>=$2 AND aff_frozen_quota>=$3`, in.UserID, available.String(), frozen.String())
		if err != nil {
			return err
		}
		count, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return service.ErrAffiliateCashUnavailable
		}
		for _, a := range allocations {
			if _, err = c.ExecContext(ctx, `UPDATE user_affiliate_ledger SET remaining_amount=remaining_amount-$2,updated_at=NOW() WHERE id=$1`, a.id, a.amount.String()); err != nil {
				return err
			}
			if _, err = c.ExecContext(ctx, `INSERT INTO affiliate_cash_allocations(application_id,credit_id,amount) VALUES($1,$2,$3)`, id, a.id, a.amount.String()); err != nil {
				return err
			}
		}
		if err = recordCashLedger(ctx, c, in.UserID, "cash_reserve", in.Amount, fmt.Sprintf("cash-reserve-%d", id)); err != nil {
			return err
		}
		result, err = getCashApplication(ctx, c, id, in.UserID)
		return err
	})
	return result, err
}

func recordCashLedger(ctx context.Context, c affiliateQueryExecer, userID int64, action, amount, operation string) error {
	_, err := c.ExecContext(ctx, `
INSERT INTO user_affiliate_ledger(user_id,action,amount,operation_id,balance_after,aff_quota_after,aff_frozen_quota_after,aff_history_quota_after)
SELECT a.user_id,$2,$3,$4,u.balance,a.aff_quota,a.aff_frozen_quota,a.aff_history_quota FROM user_affiliates a JOIN users u ON u.id=a.user_id WHERE a.user_id=$1`, userID, action, amount, operation)
	return err
}

func (r *affiliateCashRepository) List(ctx context.Context, f service.AffiliateCashFilter) ([]service.AffiliateCashApplication, int64, error) {
	c := clientFromContext(ctx, r.client)
	args := []any{f.UserID, f.Status}
	where := ` WHERE ($1::bigint=0 OR a.user_id=$1) AND ($2::text='' OR a.status=$2)`
	total, err := queryAffiliateRecordCount(ctx, c, `SELECT COUNT(*) FROM affiliate_cash_applications a`+where, args...)
	if err != nil {
		return nil, 0, err
	}
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := c.QueryContext(ctx, `SELECT `+cashApplicationColumns+` FROM affiliate_cash_applications a JOIN users u ON u.id=a.user_id`+where+` ORDER BY a.id DESC LIMIT $3 OFFSET $4`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.AffiliateCashApplication, 0)
	for rows.Next() {
		a, err := scanCashApplication(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *a)
	}
	return items, total, rows.Err()
}

func (r *affiliateCashRepository) Get(ctx context.Context, id, userID int64) (*service.AffiliateCashApplication, error) {
	c := clientFromContext(ctx, r.client)
	a, err := getCashApplication(ctx, c, id, userID)
	if err != nil {
		return nil, err
	}
	rows, err := c.QueryContext(ctx, `SELECT `+cashReplyColumns+` FROM affiliate_cash_replies WHERE application_id=$1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	a.Replies = make([]service.AffiliateCashReply, 0)
	for rows.Next() {
		reply, err := scanCashReply(rows)
		if err != nil {
			return nil, err
		}
		a.Replies = append(a.Replies, *reply)
	}
	return a, rows.Err()
}

func (r *affiliateCashRepository) QR(ctx context.Context, id, userID int64) ([]byte, string, error) {
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, `SELECT payment_qr,payment_qr_type FROM affiliate_cash_applications WHERE id=$1 AND ($2::bigint=0 OR user_id=$2)`, id, userID)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, "", err
		}
		return nil, "", service.ErrAffiliateCashNotFound
	}
	var data []byte
	var mime string
	err = rows.Scan(&data, &mime)
	return data, mime, err
}

const cashReplyColumns = `id,application_id,content,decision,CASE WHEN delivery_status='sending' AND delivery_started_at<clock_timestamp()-INTERVAL '5 minutes' THEN 'unknown' ELSE delivery_status END,created_at,sent_at,request_hash,delivery_token`

func scanCashReply(rows *sql.Rows) (*service.AffiliateCashReply, error) {
	r := new(service.AffiliateCashReply)
	err := rows.Scan(&r.ID, &r.ApplicationID, &r.Content, &r.Decision, &r.DeliveryStatus, &r.CreatedAt, &r.SentAt, &r.RequestHash, &r.DeliveryToken)
	return r, err
}

func (r *affiliateCashRepository) Reply(ctx context.Context, in service.AffiliateCashReplyInput) (*service.AffiliateCashReply, error) {
	var reply *service.AffiliateCashReply
	err := r.withTx(ctx, func(ctx context.Context, c *dbent.Client) error {
		application, err := getCashApplication(ctx, c, in.ApplicationID, 0)
		if err != nil {
			return err
		}
		if err = lockAffiliateProfile(ctx, c, application.UserID); err != nil {
			return err
		}
		rows, err := c.QueryContext(ctx, `SELECT `+cashReplyColumns+` FROM affiliate_cash_replies WHERE admin_id=$1 AND operation_id=$2`, in.AdminID, in.OperationID)
		if err != nil {
			return err
		}
		if rows.Next() {
			reply, err = scanCashReply(rows)
			_ = rows.Close()
			if err != nil {
				return err
			}
			if reply.RequestHash != in.RequestHash {
				return service.ErrIdempotencyKeyConflict
			}
			return nil
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		// Re-read status after acquiring the shared per-user quota lock.
		application, err = getCashApplication(ctx, c, in.ApplicationID, 0)
		if err != nil {
			return err
		}
		if in.Decision != "pending" {
			if application.Status != "pending" {
				return service.ErrAffiliateCashResolved
			}
			if in.Decision == "paid" {
				rows, err = c.QueryContext(ctx, `SELECT payout_not_before<=clock_timestamp() FROM affiliate_cash_applications WHERE id=$1`, in.ApplicationID)
				if err != nil {
					return err
				}
				var ready bool
				if !rows.Next() {
					_ = rows.Close()
					return service.ErrAffiliateCashNotFound
				}
				err = rows.Scan(&ready)
				_ = rows.Close()
				if err != nil {
					return err
				}
				if !ready {
					return service.ErrAffiliateCashFrozen
				}
				if err = recordCashLedger(ctx, c, application.UserID, "withdraw", application.Amount, fmt.Sprintf("cash-paid-%d", application.ID)); err != nil {
					return err
				}
			} else {
				// Return each reservation to its ORIGINAL credit and freeze
				// state. Neither earned_at nor the 24h window is renewed.
				if _, err = thawFrozenQuotaTx(ctx, c, application.UserID); err != nil {
					return err
				}
				_, err = c.ExecContext(ctx, `
WITH released AS (
 UPDATE user_affiliate_ledger l SET remaining_amount=l.remaining_amount+x.amount,updated_at=NOW()
 FROM affiliate_cash_allocations x WHERE x.application_id=$1 AND l.id=x.credit_id
 RETURNING x.amount,l.frozen_until
), totals AS (
 SELECT COALESCE(SUM(amount) FILTER(WHERE frozen_until IS NULL),0) AS available,
 COALESCE(SUM(amount) FILTER(WHERE frozen_until IS NOT NULL),0) AS frozen FROM released
)
UPDATE user_affiliates a SET aff_quota=a.aff_quota+t.available,aff_frozen_quota=a.aff_frozen_quota+t.frozen,updated_at=NOW()
FROM totals t WHERE a.user_id=$2`, in.ApplicationID, application.UserID)
				if err != nil {
					return err
				}
				if err = recordCashLedger(ctx, c, application.UserID, "cash_release", application.Amount, fmt.Sprintf("cash-release-%d", application.ID)); err != nil {
					return err
				}
			}
			if _, err = c.ExecContext(ctx, `UPDATE affiliate_cash_applications SET status=$2,resolved_at=clock_timestamp(),resolved_by=$3,updated_at=NOW() WHERE id=$1`, in.ApplicationID, in.Decision, in.AdminID); err != nil {
				return err
			}
		}
		rows, err = c.QueryContext(ctx, `
INSERT INTO affiliate_cash_replies(application_id,admin_id,content,decision,operation_id,request_hash,delivery_token)
VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING `+cashReplyColumns, in.ApplicationID, in.AdminID, in.Content, in.Decision, in.OperationID, in.RequestHash, in.DeliveryToken)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return fmt.Errorf("cash reply returned no id")
		}
		reply, err = scanCashReply(rows)
		if reply != nil {
			reply.ShouldSend = true
		}
		return err
	})
	return reply, err
}

func (r *affiliateCashRepository) ClaimReplyRetry(ctx context.Context, id, replyID int64, token string, confirm bool) (*service.AffiliateCashReply, error) {
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, `
UPDATE affiliate_cash_replies SET delivery_status='sending',delivery_token=$3,delivery_started_at=clock_timestamp()
WHERE application_id=$1 AND id=$2 AND (delivery_status='failed' OR
 ($4 AND delivery_status='sending' AND delivery_started_at<clock_timestamp()-INTERVAL '5 minutes'))
RETURNING `+cashReplyColumns, id, replyID, token, confirm)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, service.ErrAffiliateCashDelivery
	}
	return scanCashReply(rows)
}

func (r *affiliateCashRepository) FinishReply(ctx context.Context, id int64, token string, sent bool) error {
	status := "failed"
	if sent {
		status = "sent"
	}
	res, err := clientFromContext(ctx, r.client).ExecContext(ctx, `UPDATE affiliate_cash_replies SET delivery_status=$3::text,sent_at=CASE WHEN $3::text='sent' THEN clock_timestamp() ELSE NULL END WHERE id=$1 AND delivery_token=$2 AND delivery_status='sending'`, id, token, status)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return service.ErrAffiliateCashDelivery
	}
	return nil
}
