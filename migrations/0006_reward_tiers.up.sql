CREATE TABLE reward_tiers (
    id                 UUID PRIMARY KEY,
    campaign_id        UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    title              TEXT NOT NULL CHECK (length(title) BETWEEN 2 AND 80),
    description        TEXT NOT NULL,
    min_amount         BIGINT NOT NULL CHECK (min_amount >= 10000),   -- min INR 100
    quantity_limit     INTEGER CHECK (quantity_limit > 0),            -- NULL = unlimited
    claimed_count      INTEGER NOT NULL DEFAULT 0 CHECK (claimed_count >= 0),
    estimated_delivery DATE,
    grants_download    BOOLEAN NOT NULL DEFAULT FALSE,
    grants_credit      BOOLEAN NOT NULL DEFAULT FALSE,
    grants_bts         BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order         INTEGER NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_tier_not_oversold
        CHECK (quantity_limit IS NULL OR claimed_count <= quantity_limit)
);

-- chk_tier_not_oversold is the one that matters. Rule F4 - a limited tier
-- cannot be over-claimed - is enforced by the DATABASE, not by an
-- `if claimed < limit` in Go. Under concurrency the Go check races; the
-- constraint cannot. The service still takes SELECT ... FOR UPDATE on the tier
-- so the common case gets a clean 409 rather than a constraint violation, but
-- the constraint is what makes it correct. Test P7.

CREATE INDEX idx_tiers_campaign ON reward_tiers (campaign_id, sort_order);

CREATE TRIGGER trg_reward_tiers_updated
    BEFORE UPDATE ON reward_tiers
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
