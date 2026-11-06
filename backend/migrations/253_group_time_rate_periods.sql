-- Empty schedules preserve all existing group/subscription prices.
ALTER TABLE groups ADD COLUMN IF NOT EXISTS time_rate_periods JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE groups ADD CONSTRAINT groups_time_rate_periods_shape CHECK (
    jsonb_typeof(time_rate_periods) = 'array' AND jsonb_array_length(time_rate_periods) IN (0, 2)
);
COMMENT ON COLUMN groups.time_rate_periods IS
    'Two complementary daily peak/off-peak intervals in the server timezone; empty disables';

-- A dedicated trigger preserves other migrations' auth-invalidation fields.
CREATE OR REPLACE FUNCTION enqueue_group_time_rate_cache_invalidation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT encode(sha256(convert_to(k.key, 'UTF8')), 'hex')
    FROM api_keys k WHERE k.group_id = NEW.id AND k.deleted_at IS NULL AND k.key <> '';
    RETURN NEW;
END;
$$;
CREATE TRIGGER group_time_rate_cache_invalidation
    AFTER UPDATE OF time_rate_periods, recharge_rate_tiers ON groups
    FOR EACH ROW WHEN (OLD.time_rate_periods IS DISTINCT FROM NEW.time_rate_periods
                    OR OLD.recharge_rate_tiers IS DISTINCT FROM NEW.recharge_rate_tiers)
    EXECUTE FUNCTION enqueue_group_time_rate_cache_invalidation();
