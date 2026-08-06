-- Every webhook the system has ever accepted. This is the durable idempotency
-- ledger and the audit trail.
CREATE TABLE payment_events (
    id                 UUID PRIMARY KEY,
    provider           TEXT NOT NULL DEFAULT 'razorpay',
    provider_event_id  TEXT NOT NULL,
    event_type         TEXT NOT NULL,          -- payment.captured, refund.processed, ...
    pledge_id          UUID REFERENCES pledges(id),
    payload            JSONB NOT NULL,
    signature_valid    BOOLEAN NOT NULL,
    processed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_provider_event UNIQUE (provider, provider_event_id)
);

-- uq_provider_event IS the idempotency guarantee. The Redis SETNX guard in
-- pledge/webhook.go is a fast path in front of it, nothing more: Redis can lose
-- the key, and when it does this constraint is what still holds. The processing
-- transaction inserts here FIRST, so a 23505 means "already processed" and the
-- whole transaction rolls back harmlessly.

CREATE INDEX idx_payment_events_pledge ON payment_events (pledge_id, processed_at DESC);
CREATE INDEX idx_payment_events_type   ON payment_events (event_type, processed_at DESC);
