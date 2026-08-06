CREATE TABLE entitlements (
    id          UUID PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    film_id     TEXT NOT NULL,                  -- films._id in the read model
    kind        TEXT NOT NULL CHECK (kind IN ('EARLY_ACCESS','DOWNLOAD','CREDIT','BTS')),
    source_pledge_id UUID REFERENCES pledges(id),
    granted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ,                    -- NULL = permanent
    revoked_at  TIMESTAMPTZ,

    CONSTRAINT uq_entitlement UNIQUE (user_id, film_id, kind)
);

CREATE INDEX idx_entitlements_lookup ON entitlements (user_id, film_id)
    WHERE revoked_at IS NULL;
