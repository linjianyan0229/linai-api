-- Candy checks are opt-in because the external benchmark receives the upstream key.
ALTER TABLE channel_monitors ADD COLUMN IF NOT EXISTS candy_enabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS channel_monitor_candy_states (
    monitor_id BIGINT PRIMARY KEY REFERENCES channel_monitors(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    last_started_at TIMESTAMPTZ NOT NULL,
    running_until TIMESTAMPTZ,
    history JSONB NOT NULL DEFAULT '[]'::jsonb,
    CONSTRAINT channel_monitor_candy_history_array CHECK (jsonb_typeof(history) = 'array')
);
