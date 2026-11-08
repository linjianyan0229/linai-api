ALTER TABLE announcements ADD COLUMN is_pinned BOOLEAN NOT NULL DEFAULT FALSE;

-- Apply priority before the active-announcement limit, including older pins.
CREATE INDEX idx_announcements_pinned_active
    ON announcements (is_pinned DESC, id DESC) WHERE status = 'active';

COMMENT ON COLUMN announcements.is_pinned IS '置顶优先展示，不改变投放条件或已读状态';
