CREATE TABLE idempotency_keys (
    key           TEXT NOT NULL,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint      TEXT NOT NULL,
    request_hash  TEXT NOT NULL,                -- SHA-256 of the canonical request body
    status        TEXT NOT NULL DEFAULT 'IN_FLIGHT'
                       CHECK (status IN ('IN_FLIGHT','COMPLETED')),
    response_code INTEGER,
    response_body JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (user_id, endpoint, key)
);

-- Scoping by user_id is a security property, not tidiness: a global key
-- namespace lets user A guess user B's key and read B's cached response body.

CREATE INDEX idx_idem_expiry ON idempotency_keys (expires_at);
