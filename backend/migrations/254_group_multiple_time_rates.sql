ALTER TABLE groups DROP CONSTRAINT groups_time_rate_periods_shape;
ALTER TABLE groups ADD CONSTRAINT groups_time_rate_periods_shape CHECK (
    jsonb_typeof(time_rate_periods) = 'array' AND (
        jsonb_array_length(time_rate_periods) = 0
        OR jsonb_array_length(time_rate_periods) BETWEEN 2 AND 48
    )
);
COMMENT ON COLUMN groups.time_rate_periods IS
    'Daily peak/off-peak intervals in Asia/Shanghai; 2-48 periods cover the day, empty disables';
