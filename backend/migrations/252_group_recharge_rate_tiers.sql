-- Opt-in, per-group lifetime recharge levels. Existing groups keep their rates.
ALTER TABLE groups ADD COLUMN IF NOT EXISTS recharge_rate_tiers JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE groups ADD CONSTRAINT groups_recharge_rate_tiers_shape CHECK (
    jsonb_typeof(recharge_rate_tiers) = 'array'
    AND jsonb_array_length(recharge_rate_tiers) IN (0, 3)
);
COMMENT ON COLUMN groups.recharge_rate_tiers IS
    'Three cumulative balance-recharge USD principal thresholds and absolute group rates; empty disables';
