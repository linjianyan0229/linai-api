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

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestAffiliateCashPostgresContract(t *testing.T) {
	dsn := os.Getenv("LINAI_AFFILIATE_TEST_DSN")
	if dsn == "" {
		t.Skip("set LINAI_AFFILIATE_TEST_DSN to run isolated cash accounting contract")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = admin.Close() }()
	schema := fmt.Sprintf("cash_contract_%d", time.Now().UnixNano())
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
	_, err = db.ExecContext(ctx, `
CREATE TABLE users(id BIGINT PRIMARY KEY,email TEXT,username TEXT,balance NUMERIC(20,8) NOT NULL DEFAULT 0,total_recharged NUMERIC(20,8) NOT NULL DEFAULT 0,updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),deleted_at TIMESTAMPTZ);
CREATE TABLE user_affiliates(user_id BIGINT PRIMARY KEY REFERENCES users(id),aff_code TEXT UNIQUE,aff_code_custom BOOLEAN NOT NULL DEFAULT false,aff_rebate_rate_percent NUMERIC,inviter_id BIGINT,aff_count INT NOT NULL DEFAULT 0,aff_quota NUMERIC(20,8) NOT NULL DEFAULT 0,aff_frozen_quota NUMERIC(20,8) NOT NULL DEFAULT 0,aff_history_quota NUMERIC(20,8) NOT NULL DEFAULT 0,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
CREATE TABLE user_affiliate_ledger(id BIGSERIAL PRIMARY KEY,user_id BIGINT NOT NULL REFERENCES users(id),action TEXT NOT NULL,amount NUMERIC(20,8) NOT NULL,source_user_id BIGINT,source_order_id BIGINT,frozen_until TIMESTAMPTZ,operation_id VARCHAR(64),balance_after NUMERIC(20,8),aff_quota_after NUMERIC(20,8),aff_frozen_quota_after NUMERIC(20,8),aff_history_quota_after NUMERIC(20,8),created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
CREATE UNIQUE INDEX idx_user_affiliate_ledger_operation_id ON user_affiliate_ledger(operation_id) WHERE operation_id IS NOT NULL;
INSERT INTO users(id,email,username) SELECT x,'cash'||x||'@example.test','cash'||x FROM generate_series(1,8) x;
INSERT INTO user_affiliates(user_id,aff_code,aff_quota,aff_frozen_quota,aff_history_quota) VALUES(1,'CASH1',7,5,25);
INSERT INTO user_affiliate_ledger(user_id,action,amount,created_at,frozen_until) VALUES
(1,'accrue',10,NOW()-INTERVAL '2 days',NULL),
(1,'accrue',5,NOW()-INTERVAL '1 hour',NULL),
(1,'accrue',10,NOW()-INTERVAL '1 hour',NOW()+INTERVAL '2 days');`)
	require.NoError(t, err)
	migration, err := os.ReadFile("../../migrations/250_affiliate_cash_withdrawals.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := NewAffiliateCashRepository(client)
	legacy := NewAffiliateRepository(client, db)
	scalar := func(query string, args ...any) string {
		var value string
		require.NoError(t, db.QueryRowContext(ctx, query, args...).Scan(&value))
		return value
	}
	summary, err := repo.Summary(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "10.00000000", summary.EligibleAmount)
	require.Equal(t, "2.00000000", scalar(`SELECT remaining_amount::text FROM user_affiliate_ledger WHERE user_id=1 ORDER BY id LIMIT 1`), "migration backfills oldest-first consumption")
	input := service.AffiliateCashCreate{UserID: 1, Email: "cash@example.test", Amount: "8", QR: []byte("private-png"), QRType: "image/png", OperationID: "apply1", RequestHash: "hash1"}
	application, err := repo.Create(ctx, input)
	require.NoError(t, err)
	require.Equal(t, "8.00000000", application.Amount)
	require.Equal(t, "2.00000000", scalar(`SELECT aff_quota::text FROM user_affiliates WHERE user_id=1`))
	require.Equal(t, "2.00000000", scalar(`SELECT aff_frozen_quota::text FROM user_affiliates WHERE user_id=1`))
	replay, err := repo.Create(ctx, input)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, application.ID, replay.ID)
	input.Amount = "7"
	input.RequestHash = "different"
	_, err = repo.Create(ctx, input)
	require.ErrorIs(t, err, service.ErrIdempotencyKeyConflict)
	qr, _, err := repo.QR(ctx, application.ID, 1)
	require.NoError(t, err)
	require.Equal(t, []byte("private-png"), qr)
	_, _, err = repo.QR(ctx, application.ID, 2)
	require.ErrorIs(t, err, service.ErrAffiliateCashNotFound)
	_, err = repo.Get(ctx, application.ID, 2)
	require.ErrorIs(t, err, service.ErrAffiliateCashNotFound)
	rows, total, err := repo.List(ctx, service.AffiliateCashFilter{UserID: 2, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, rows)
	transferred, _, err := legacy.TransferQuotaToBalance(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 2.0, transferred, "reserved credits cannot be transferred")
	pay := service.AffiliateCashReplyInput{ApplicationID: application.ID, AdminID: 8, Content: "paid", Decision: "paid", OperationID: "paid1", RequestHash: "paid1", DeliveryToken: "send1"}
	_, err = repo.Reply(ctx, pay)
	require.ErrorIs(t, err, service.ErrAffiliateCashFrozen)
	// Expiration does not cancel a reservation. Rejecting returns the original
	// timestamps, so late returns are transferable but not cash-eligible.
	_, err = db.ExecContext(ctx, `UPDATE user_affiliate_ledger SET created_at=NOW()-INTERVAL '25 hours' WHERE user_id=1`)
	require.NoError(t, err)
	pay.Decision = "rejected"
	pay.Content = "rejected"
	pay.OperationID = "reject1"
	pay.RequestHash = "reject1"
	reply, err := repo.Reply(ctx, pay)
	require.NoError(t, err)
	require.True(t, reply.ShouldSend)
	duplicate, err := repo.Reply(ctx, pay)
	require.NoError(t, err)
	require.False(t, duplicate.ShouldSend)
	require.Equal(t, "5.00000000", scalar(`SELECT aff_quota::text FROM user_affiliates WHERE user_id=1`))
	require.Equal(t, "5.00000000", scalar(`SELECT aff_frozen_quota::text FROM user_affiliates WHERE user_id=1`))
	summary, err = repo.Summary(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "0", summary.EligibleAmount)
	require.NoError(t, repo.FinishReply(ctx, reply.ID, "send1", false))
	require.ErrorIs(t, repo.FinishReply(ctx, reply.ID, "send1", true), service.ErrAffiliateCashDelivery)
	retry, err := repo.ClaimReplyRetry(ctx, application.ID, reply.ID, "retry1", false)
	require.NoError(t, err)
	_, err = repo.ClaimReplyRetry(ctx, application.ID, reply.ID, "retry2", false)
	require.ErrorIs(t, err, service.ErrAffiliateCashDelivery)
	require.NoError(t, repo.FinishReply(ctx, retry.ID, "retry1", true))
	_, err = repo.ClaimReplyRetry(ctx, application.ID, reply.ID, "retry3", true)
	require.ErrorIs(t, err, service.ErrAffiliateCashDelivery)

	// A single exact-boundary credit is never cash-eligible, even on replay of
	// a transaction timestamp. Just-inside and just-outside portions are split.
	_, err = legacy.EnsureUserAffiliate(ctx, 2)
	require.NoError(t, err)
	_, err = legacy.AccrueQuota(ctx, 2, 3, 3, 0, nil)
	require.NoError(t, err)
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	txCtx := dbent.NewTxContext(ctx, tx)
	_, err = tx.Client().ExecContext(txCtx, `UPDATE user_affiliate_ledger SET created_at=NOW()-INTERVAL '24 hours' WHERE user_id=2`)
	require.NoError(t, err)
	summary, err = repo.Summary(txCtx, 2)
	require.NoError(t, err)
	require.Equal(t, "0", summary.EligibleAmount)
	require.NoError(t, tx.Rollback())
	input.UserID = 2
	input.Amount = "3"
	input.RequestHash = "cash2"
	input.OperationID = "cash2"
	second, err := repo.Create(ctx, input)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE user_affiliate_ledger SET created_at=NOW()-INTERVAL '25 hours' WHERE user_id=2`)
	require.NoError(t, err)
	pay.ApplicationID = second.ID
	pay.Decision = "paid"
	pay.OperationID = "paid2"
	pay.RequestHash = "paid2"
	_, err = repo.Reply(ctx, pay)
	require.NoError(t, err, "timely applications can be paid after expiration")
	_, err = repo.Reply(ctx, pay)
	require.NoError(t, err)
	require.Equal(t, "1", scalar(`SELECT COUNT(*)::text FROM user_affiliate_ledger WHERE user_id=2 AND action='withdraw'`))

	// Concurrent applications, transfers and the legacy offline withdrawal all
	// compete for one pool. Exactly ten dollars may leave it, never more.
	_, err = legacy.EnsureUserAffiliate(ctx, 3)
	require.NoError(t, err)
	_, err = legacy.AccrueQuota(ctx, 3, 4, 10, 0, nil)
	require.NoError(t, err)
	var workers sync.WaitGroup
	failures := make(chan error, 24)
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			request := input
			request.UserID = 3
			request.Amount = "1"
			request.OperationID = fmt.Sprintf("concurrent-%d", i)
			request.RequestHash = request.OperationID
			_, err := repo.Create(ctx, request)
			if err != nil && err != service.ErrAffiliateCashUnavailable {
				failures <- err
			}
		}(i)
	}
	workers.Add(2)
	go func() {
		defer workers.Done()
		_, _, err := legacy.TransferQuotaToBalance(ctx, 3)
		if err != nil && err != service.ErrAffiliateQuotaEmpty {
			failures <- err
		}
	}()
	go func() {
		defer workers.Done()
		_, err := legacy.WithdrawQuota(ctx, 3, 1, "legacy-concurrent")
		if err != nil && err != service.ErrAffiliateQuotaInsufficient {
			failures <- err
		}
	}()
	workers.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	require.Equal(t, "10.00000000", scalar(`SELECT (COALESCE((SELECT SUM(amount) FROM affiliate_cash_applications WHERE user_id=3),0)+COALESCE((SELECT SUM(amount) FROM user_affiliate_ledger WHERE user_id=3 AND action IN ('transfer','withdraw')),0))::text`))
	require.Equal(t, "0.00000000", scalar(`SELECT aff_quota::text FROM user_affiliates WHERE user_id=3`))
	require.Equal(t, "0.00000000", scalar(`SELECT SUM(remaining_amount)::text FROM user_affiliate_ledger WHERE user_id=3 AND action='accrue'`))

	// The first paid reply must be the only payout transition, even when two
	// administrators race with different operation keys.
	_, err = legacy.EnsureUserAffiliate(ctx, 4)
	require.NoError(t, err)
	_, err = legacy.AccrueQuota(ctx, 4, 5, 1, 0, nil)
	require.NoError(t, err)
	input.UserID = 4
	input.Amount = "1"
	input.OperationID = "race-pay"
	input.RequestHash = "race-pay"
	third, err := repo.Create(ctx, input)
	require.NoError(t, err)
	wins := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			request := pay
			request.ApplicationID = third.ID
			request.AdminID = int64(7 + i)
			request.OperationID = fmt.Sprintf("pay-%d", i)
			request.RequestHash = request.OperationID
			_, err := repo.Reply(ctx, request)
			wins <- err == nil
		}(i)
	}
	workers.Wait()
	close(wins)
	count := 0
	for won := range wins {
		if won {
			count++
		}
	}
	require.Equal(t, 1, count)
	require.Equal(t, "1", scalar(`SELECT COUNT(*)::text FROM user_affiliate_ledger WHERE user_id=4 AND action='withdraw'`))
}

func TestAffiliateCashFullMigrationPostgresContract(t *testing.T) {
	dsn := os.Getenv("LINAI_AFFILIATE_TEST_DSN")
	if dsn == "" {
		t.Skip("set LINAI_AFFILIATE_TEST_DSN for real migration verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = admin.Close() }()
	schema := fmt.Sprintf("cash_full_%d", time.Now().UnixNano())
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
	require.NoError(t, ApplyMigrations(ctx, db))
	require.NoError(t, ApplyMigrations(ctx, db), "restart must not apply migration twice")
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	var owner int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,role,status) VALUES('cash-full@example.test','test-hash','admin','active') RETURNING id`).Scan(&owner))
	legacy := NewAffiliateRepository(client, db)
	_, err = legacy.EnsureUserAffiliate(ctx, owner)
	require.NoError(t, err)
	_, err = legacy.AccrueQuota(ctx, owner, owner, 10.25, 0, nil)
	require.NoError(t, err)
	cash := NewAffiliateCashRepository(client)
	a, err := cash.Create(ctx, service.AffiliateCashCreate{UserID: owner, Email: "cash@example.test", Amount: "4.25", QR: []byte("private-png"), QRType: "image/png", OperationID: "cash-full", RequestHash: "cash-full"})
	require.NoError(t, err)
	amount, _, err := legacy.TransferQuotaToBalance(ctx, owner)
	require.NoError(t, err)
	require.Equal(t, 6.0, amount)
	reply, err := cash.Reply(ctx, service.AffiliateCashReplyInput{ApplicationID: a.ID, AdminID: owner, Content: "Paid offline", Decision: "paid", OperationID: "cash-full-paid", RequestHash: "cash-full-paid", DeliveryToken: "cash-full-send"})
	require.NoError(t, err)
	require.True(t, reply.ShouldSend)
	require.NoError(t, cash.FinishReply(ctx, reply.ID, reply.DeliveryToken, true))
	detail, err := cash.Get(ctx, a.ID, owner)
	require.NoError(t, err)
	require.Equal(t, "paid", detail.Status)
	require.Len(t, detail.Replies, 1)
	require.Equal(t, "sent", detail.Replies[0].DeliveryStatus)
}
