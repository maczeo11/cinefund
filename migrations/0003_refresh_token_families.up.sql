CREATE TABLE refresh_token_families (
    id           UUID PRIMARY KEY,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    current_jti  UUID NOT NULL,                 -- the only jti currently valid
    user_agent   TEXT,
    ip           INET,
    revoked_at   TIMESTAMPTZ,                   -- non-null = family burned
    revoke_reason TEXT CHECK (revoke_reason IN ('LOGOUT','REUSE_DETECTED','ADMIN','EXPIRED')),
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- current_jti UNIQUE makes rotation a compare-and-swap: UPDATE ... WHERE
-- current_jti = J1 rotates to J2, and a second concurrent refresh with J1
-- affects zero rows, which is the reuse signal.
CREATE INDEX idx_rtf_user_active ON refresh_token_families (user_id)
    WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX idx_rtf_current_jti ON refresh_token_families (current_jti);

CREATE TRIGGER trg_refresh_token_families_updated
    BEFORE UPDATE ON refresh_token_families
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
