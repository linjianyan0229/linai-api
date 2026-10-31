CREATE TABLE announcement_email_subscriptions (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    enabled_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    locale VARCHAR(8) NOT NULL DEFAULT 'zh',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

ALTER TABLE announcements ADD COLUMN email_revision BIGINT NOT NULL DEFAULT 0;

CREATE TABLE announcement_email_events (
    id BIGSERIAL PRIMARY KEY,
    announcement_id BIGINT NOT NULL REFERENCES announcements(id) ON DELETE CASCADE,
    revision BIGINT NOT NULL,
    published_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    cursor_user_id BIGINT NOT NULL DEFAULT 0,
    fanout_done BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE(announcement_id, revision)
);

CREATE TABLE announcement_email_deliveries (
    id BIGSERIAL PRIMARY KEY,
    event_id BIGINT NOT NULL REFERENCES announcement_email_events(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status VARCHAR(16) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'sending', 'sent', 'skipped', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    lease_until TIMESTAMPTZ,
    lease_token VARCHAR(64),
    sent_at TIMESTAMPTZ,
    UNIQUE(event_id, user_id)
);
CREATE INDEX idx_announcement_email_fanout ON announcement_email_events(id)
    WHERE fanout_done = FALSE;
CREATE INDEX idx_announcement_email_due ON announcement_email_deliveries(next_attempt_at, id)
    WHERE status IN ('pending', 'sending');

-- Publication events are committed with their announcement. Metadata-only saves
-- do not send another email; archiving invalidates any outstanding revision.
CREATE FUNCTION track_announcement_email_revision() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.email_revision := 1;
    ELSIF ROW(NEW.title, NEW.content, NEW.targeting, NEW.status, NEW.starts_at, NEW.ends_at)
        IS DISTINCT FROM ROW(OLD.title, OLD.content, OLD.targeting, OLD.status, OLD.starts_at, OLD.ends_at) THEN
        NEW.email_revision := OLD.email_revision + 1;
    ELSE
        NEW.email_revision := OLD.email_revision;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION queue_announcement_email_event() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.status = 'active' THEN
        IF TG_OP = 'INSERT' THEN
            INSERT INTO announcement_email_events(announcement_id, revision)
                VALUES(NEW.id, NEW.email_revision);
        ELSIF NEW.email_revision <> OLD.email_revision THEN
            INSERT INTO announcement_email_events(announcement_id, revision)
                VALUES(NEW.id, NEW.email_revision);
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER announcement_email_revision BEFORE INSERT OR UPDATE ON announcements
    FOR EACH ROW EXECUTE FUNCTION track_announcement_email_revision();
CREATE TRIGGER announcement_email_publication AFTER INSERT OR UPDATE ON announcements
    FOR EACH ROW EXECUTE FUNCTION queue_announcement_email_event();
