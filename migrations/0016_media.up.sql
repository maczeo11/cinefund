-- Media assets and transcode jobs.
--
-- These were Mongo collections in the original design (docs/03). ADR-0010 moved
-- them into Postgres: the genuinely document-shaped parts - raw ffprobe output
-- and the per-rung rendition list - are JSONB, and everything that is queried or
-- constrained is a real column.

CREATE TABLE media_assets (
    id              UUID PRIMARY KEY,
    owner_id        UUID NOT NULL REFERENCES users(id),
    campaign_id     UUID REFERENCES campaigns(id),
    purpose         TEXT NOT NULL CHECK (purpose IN ('PITCH','FILM','TRAILER','BTS')),

    -- Server-generated. Never derived from a client-supplied filename.
    storage_key     TEXT NOT NULL UNIQUE,
    original_name   TEXT,
    content_type    TEXT NOT NULL,
    size_bytes      BIGINT CHECK (size_bytes > 0),

    status          TEXT NOT NULL DEFAULT 'PENDING_UPLOAD' CHECK (status IN
                        ('PENDING_UPLOAD','UPLOADED','PROBING','TRANSCODING',
                         'READY','REJECTED','FAILED')),
    reject_reason   TEXT,

    -- Raw ffprobe JSON, kept verbatim. When a file misbehaves six months later
    -- this is the only thing that explains why the ladder came out as it did.
    probe           JSONB,
    duration_secs   DOUBLE PRECISION CHECK (duration_secs > 0),
    width           INTEGER,
    height          INTEGER,
    rotation        INTEGER NOT NULL DEFAULT 0 CHECK (rotation IN (0,90,180,270)),

    -- [{"name":"720p","height":720,"bandwidth":2996000,"key":"..."}...]
    renditions      JSONB NOT NULL DEFAULT '[]'::jsonb,
    poster_key      TEXT,
    master_key      TEXT,
    pipeline_version INTEGER NOT NULL DEFAULT 1,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_ready_has_master
        CHECK (status <> 'READY' OR master_key IS NOT NULL),
    CONSTRAINT chk_rejected_has_reason
        CHECK (status <> 'REJECTED' OR reject_reason IS NOT NULL)
);

CREATE INDEX idx_media_owner  ON media_assets (owner_id, created_at DESC);
CREATE INDEX idx_media_status ON media_assets (status);

-- Sweep index: assets stuck in UPLOADED are picked up by the scheduler if the
-- outbox event was somehow never produced.
CREATE INDEX idx_media_stuck ON media_assets (updated_at) WHERE status = 'UPLOADED';

CREATE TRIGGER trg_media_assets_updated
    BEFORE UPDATE ON media_assets
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE transcode_jobs (
    id               UUID PRIMARY KEY,
    asset_id         UUID NOT NULL REFERENCES media_assets(id) ON DELETE CASCADE,
    pipeline_version INTEGER NOT NULL DEFAULT 1,

    status           TEXT NOT NULL DEFAULT 'QUEUED' CHECK (status IN
                         ('QUEUED','RUNNING','SUCCEEDED','FAILED','CANCELLED')),

    -- Lease. worker_id is the fencing token: a heartbeat filtered on it matches
    -- zero rows once another worker has reclaimed the job, and that is the
    -- signal to abort rather than keep writing the same rendition keys.
    worker_id        TEXT,
    lease_expires_at TIMESTAMPTZ,
    attempt          INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),

    progress         DOUBLE PRECISION NOT NULL DEFAULT 0
                         CHECK (progress >= 0 AND progress <= 1),
    speed            DOUBLE PRECISION,
    tasks            JSONB NOT NULL DEFAULT '[]'::jsonb,   -- per-rung progress
    error            TEXT,

    started_at       TIMESTAMPTZ,
    finished_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- M8: the same Kafka message delivered twice produces ONE job. Delivery is
    -- at-least-once, so this constraint is the thing that makes the consumer
    -- idempotent rather than a hopeful `if not exists` check in Go.
    CONSTRAINT uq_job_asset_version UNIQUE (asset_id, pipeline_version)
);

-- The claim query orders by this: queued work first, then expired leases.
CREATE INDEX idx_jobs_claimable ON transcode_jobs (status, lease_expires_at)
    WHERE status IN ('QUEUED','RUNNING');

CREATE TRIGGER trg_transcode_jobs_updated
    BEFORE UPDATE ON transcode_jobs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
