ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS concurrency INTEGER NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'api_keys_concurrency_valid'
          AND conrelid = 'api_keys'::regclass
    ) THEN
        ALTER TABLE api_keys ADD CONSTRAINT api_keys_concurrency_valid
            CHECK (concurrency BETWEEN 0 AND 100000);
    END IF;
END;
$$;

COMMENT ON COLUMN api_keys.concurrency IS
    'Independent concurrent request limit; 0 adds no limit to the user total';

CREATE OR REPLACE FUNCTION enqueue_api_key_concurrency_cache_invalidation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM enqueue_auth_cache_invalidation(NEW.key);
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_api_keys_concurrency_cache_invalidation ON api_keys;
CREATE TRIGGER trg_api_keys_concurrency_cache_invalidation
    AFTER UPDATE OF concurrency ON api_keys
    FOR EACH ROW WHEN (OLD.concurrency IS DISTINCT FROM NEW.concurrency)
    EXECUTE FUNCTION enqueue_api_key_concurrency_cache_invalidation();
