-- Track unspent portions of individual credits. Reconstruct legacy consumption
-- oldest-first within each existing quota pool; never invent missing credits.
ALTER TABLE user_affiliate_ledger
    ADD COLUMN remaining_amount NUMERIC(20,8) NOT NULL DEFAULT 0;

WITH credits AS (
    SELECT l.id, l.amount,
           CASE WHEN l.frozen_until IS NULL THEN a.aff_quota ELSE a.aff_frozen_quota END AS pool,
           COALESCE(SUM(l.amount) OVER (
               PARTITION BY l.user_id, (l.frozen_until IS NULL)
               ORDER BY l.created_at DESC, l.id DESC
               ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING), 0) AS newer
    FROM user_affiliate_ledger l
    JOIN user_affiliates a ON a.user_id = l.user_id
    WHERE l.action = 'accrue'
)
UPDATE user_affiliate_ledger l
SET remaining_amount = LEAST(c.amount, GREATEST(c.pool - c.newer, 0))
FROM credits c WHERE c.id = l.id;

ALTER TABLE user_affiliate_ledger ADD CONSTRAINT affiliate_credit_remaining_valid
    CHECK (remaining_amount >= 0 AND
           ((action = 'accrue' AND remaining_amount <= amount) OR
            (action <> 'accrue' AND remaining_amount = 0)));
CREATE INDEX idx_affiliate_unspent_credit
    ON user_affiliate_ledger(user_id, created_at, id)
    WHERE action = 'accrue' AND remaining_amount > 0;

CREATE TABLE affiliate_cash_applications (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    email VARCHAR(254) NOT NULL,
    amount NUMERIC(20,8) NOT NULL CHECK (amount > 0),
    currency VARCHAR(3) NOT NULL DEFAULT 'USD' CHECK (currency = 'USD'),
    payment_qr BYTEA NOT NULL CHECK (octet_length(payment_qr) BETWEEN 1 AND 2097152),
    payment_qr_type VARCHAR(32) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'paid', 'rejected')),
    operation_id VARCHAR(64) NOT NULL,
    request_hash VARCHAR(64) NOT NULL,
    payout_not_before TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ,
    resolved_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    UNIQUE(user_id, operation_id)
);
CREATE INDEX idx_affiliate_cash_admin_list ON affiliate_cash_applications(status, id DESC);
CREATE INDEX idx_affiliate_cash_user_list ON affiliate_cash_applications(user_id, id DESC);

CREATE TABLE affiliate_cash_allocations (
    application_id BIGINT NOT NULL REFERENCES affiliate_cash_applications(id) ON DELETE RESTRICT,
    credit_id BIGINT NOT NULL REFERENCES user_affiliate_ledger(id) ON DELETE RESTRICT,
    amount NUMERIC(20,8) NOT NULL CHECK (amount > 0),
    PRIMARY KEY(application_id, credit_id)
);

CREATE TABLE affiliate_cash_replies (
    id BIGSERIAL PRIMARY KEY,
    application_id BIGINT NOT NULL REFERENCES affiliate_cash_applications(id) ON DELETE RESTRICT,
    admin_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    content TEXT NOT NULL CHECK (char_length(content) BETWEEN 1 AND 4000),
    decision VARCHAR(16) NOT NULL CHECK (decision IN ('pending', 'paid', 'rejected')),
    operation_id VARCHAR(64) NOT NULL,
    request_hash VARCHAR(64) NOT NULL,
    delivery_status VARCHAR(16) NOT NULL DEFAULT 'sending' CHECK (delivery_status IN ('sending', 'sent', 'failed')),
    delivery_token VARCHAR(64) NOT NULL,
    delivery_started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(admin_id, operation_id)
);
CREATE INDEX idx_affiliate_cash_reply_history ON affiliate_cash_replies(application_id, id);
