ALTER TABLE groups ADD COLUMN dynamic_rate_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE groups ADD COLUMN dynamic_rate_factor NUMERIC(8,4) NOT NULL DEFAULT 2;
ALTER TABLE groups ADD CONSTRAINT groups_dynamic_rate_valid CHECK (
    dynamic_rate_factor > 0 AND dynamic_rate_factor <= 9999.9999
    AND (NOT dynamic_rate_enabled OR platform = 'openai')
);

CREATE OR REPLACE FUNCTION enqueue_group_dynamic_rate_cache_invalidation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT encode(sha256(convert_to(k.key, 'UTF8')), 'hex')
    FROM api_keys k WHERE k.group_id = NEW.id AND k.deleted_at IS NULL AND k.key <> '';
    RETURN NEW;
END;
$$;
CREATE TRIGGER group_dynamic_rate_cache_invalidation
    AFTER UPDATE OF dynamic_rate_enabled, dynamic_rate_factor ON groups
    FOR EACH ROW WHEN (OLD.dynamic_rate_enabled IS DISTINCT FROM NEW.dynamic_rate_enabled
                    OR OLD.dynamic_rate_factor IS DISTINCT FROM NEW.dynamic_rate_factor)
    EXECUTE FUNCTION enqueue_group_dynamic_rate_cache_invalidation();
