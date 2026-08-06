CREATE TABLE creator_profiles (
    id            UUID PRIMARY KEY,
    user_id       UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    bio           TEXT NOT NULL CHECK (length(bio) <= 2000),
    portfolio_url TEXT,
    status        TEXT NOT NULL DEFAULT 'PENDING'
                       CHECK (status IN ('PENDING','APPROVED','REJECTED')),
    review_note   TEXT,
    reviewed_by   UUID REFERENCES users(id),
    reviewed_at   TIMESTAMPTZ,
    payout_upi    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TRIGGER trg_creator_profiles_updated
    BEFORE UPDATE ON creator_profiles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
