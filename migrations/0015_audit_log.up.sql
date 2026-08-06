CREATE TABLE audit_log (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id    UUID REFERENCES users(id),
    actor_role  TEXT NOT NULL,
    action      TEXT NOT NULL,                  -- 'campaign.approve', 'refund.manual'
    target_type TEXT NOT NULL,
    target_id   TEXT NOT NULL,
    before      JSONB,
    after       JSONB,
    ip          INET,
    request_id  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_target ON audit_log (target_type, target_id, created_at DESC);
CREATE INDEX idx_audit_actor  ON audit_log (actor_id, created_at DESC);
