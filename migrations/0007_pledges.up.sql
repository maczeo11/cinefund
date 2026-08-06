CREATE TABLE pledges (
    id                  UUID PRIMARY KEY,
    campaign_id         UUID NOT NULL REFERENCES campaigns(id),
    backer_id           UUID NOT NULL REFERENCES users(id),
    tier_id             UUID REFERENCES reward_tiers(id),
    amount              BIGINT NOT NULL CHECK (amount > 0),
    currency            TEXT NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
    anonymous           BOOLEAN NOT NULL DEFAULT FALSE,
    message             TEXT CHECK (length(message) <= 500),

    status              TEXT NOT NULL DEFAULT 'CREATED' CHECK (status IN
                            ('CREATED','AUTHORIZED','CAPTURED','FAILED',
                             'REFUND_PENDING','REFUNDED','REFUND_FAILED','SETTLED')),

    provider            TEXT NOT NULL DEFAULT 'razorpay',
    provider_order_id   TEXT UNIQUE,
    provider_payment_id TEXT UNIQUE,

    captured_at         TIMESTAMPTZ,
    refunded_at         TIMESTAMPTZ,
    failure_reason      TEXT,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_captured_has_payment
        CHECK (status NOT IN ('CAPTURED','SETTLED') OR provider_payment_id IS NOT NULL),
    CONSTRAINT chk_captured_has_time
        CHECK (status NOT IN ('CAPTURED','SETTLED') OR captured_at IS NOT NULL)
);

-- provider_payment_id UNIQUE is quietly one of the most important constraints
-- here: one provider payment can attach to at most one pledge, ever. A replayed
-- webhook that tries to attach it twice fails instead of double-crediting.

CREATE INDEX idx_pledges_campaign_status ON pledges (campaign_id, status);
CREATE INDEX idx_pledges_backer          ON pledges (backer_id, created_at DESC);

-- Reconciliation sweep: anything stuck in CREATED past 15 minutes gets its true
-- status pulled from the provider's API.
CREATE INDEX idx_pledges_stale ON pledges (created_at) WHERE status = 'CREATED';

CREATE TRIGGER trg_pledges_updated
    BEFORE UPDATE ON pledges
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
