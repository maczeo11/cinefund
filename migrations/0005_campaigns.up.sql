CREATE TABLE campaigns (
    id             UUID PRIMARY KEY,
    creator_id     UUID NOT NULL REFERENCES users(id),
    slug           TEXT NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{2,79}$'),
    title          TEXT NOT NULL CHECK (length(title) BETWEEN 3 AND 120),
    tagline        TEXT NOT NULL CHECK (length(tagline) <= 200),
    synopsis       TEXT NOT NULL,
    risks          TEXT,
    category       TEXT NOT NULL CHECK (category IN
                        ('DRAMA','COMEDY','DOCUMENTARY','ANIMATION','HORROR','SCIFI','EXPERIMENTAL')),
    language       TEXT NOT NULL DEFAULT 'en',

    goal_amount    BIGINT NOT NULL CHECK (goal_amount >= 100000),   -- min INR 1,000
    raised_amount  BIGINT NOT NULL DEFAULT 0 CHECK (raised_amount >= 0),
    backer_count   INTEGER NOT NULL DEFAULT 0 CHECK (backer_count >= 0),
    currency       TEXT NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),

    status         TEXT NOT NULL DEFAULT 'DRAFT' CHECK (status IN
                        ('DRAFT','IN_REVIEW','LIVE','FUNDED','FAILED','CANCELLED',
                         'IN_PRODUCTION','RELEASED')),
    duration_days  INTEGER CHECK (duration_days BETWEEN 7 AND 90),
    published_at   TIMESTAMPTZ,
    deadline       TIMESTAMPTZ,
    finalized_at   TIMESTAMPTZ,

    cover_key      TEXT,
    -- Was a Mongo media_assets._id. Postgres-only now (ADR-0001, reversed
    -- 2026-08-06): this becomes a real FK once 00xx_media_assets lands.
    pitch_asset_id TEXT,
    review_note    TEXT,

    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_live_has_deadline
        CHECK (status <> 'LIVE' OR (deadline IS NOT NULL AND published_at IS NOT NULL))
);

-- Partial: the deadline sweep runs every 60s forever and should touch an index
-- containing only live campaigns, not every campaign ever created.
CREATE INDEX idx_campaigns_status_deadline ON campaigns (status, deadline)
    WHERE status = 'LIVE';
CREATE INDEX idx_campaigns_creator  ON campaigns (creator_id, created_at DESC);
CREATE INDEX idx_campaigns_category ON campaigns (category, status);

CREATE TRIGGER trg_campaigns_updated
    BEFORE UPDATE ON campaigns
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
