CREATE TABLE refunds (
    id                 UUID PRIMARY KEY,
    pledge_id          UUID NOT NULL REFERENCES pledges(id),
    amount             BIGINT NOT NULL CHECK (amount > 0),
    reason             TEXT NOT NULL CHECK (reason IN
                           ('CAMPAIGN_FAILED','CAMPAIGN_CANCELLED','BACKER_CANCELLED','ADMIN')),
    status             TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN
                           ('PENDING','PROCESSING','COMPLETED','FAILED')),
    provider_refund_id TEXT UNIQUE,
    idempotency_key    TEXT NOT NULL UNIQUE,   -- sent to the provider, derived from pledge_id
    failure_reason     TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- At most one non-failed refund per pledge. A failed refund can be retried as a
-- new row; a completed one blocks any further attempt. Getting this wrong means
-- paying a backer twice, which nobody will ever tell you about.
CREATE UNIQUE INDEX uq_refund_active_per_pledge ON refunds (pledge_id)
    WHERE status <> 'FAILED';

CREATE TRIGGER trg_refunds_updated
    BEFORE UPDATE ON refunds
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
