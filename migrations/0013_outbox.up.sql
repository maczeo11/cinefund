CREATE TABLE outbox (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id       UUID NOT NULL UNIQUE,
    event_type     TEXT NOT NULL,               -- 'pledge.captured'
    event_version  INTEGER NOT NULL DEFAULT 1,
    aggregate_type TEXT NOT NULL,               -- 'pledge'
    aggregate_id   UUID NOT NULL,               -- Kafka partition key
    payload        JSONB NOT NULL,
    trace_id       TEXT,                        -- W3C traceparent
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at   TIMESTAMPTZ,
    attempts       INTEGER NOT NULL DEFAULT 0,
    last_error     TEXT
);

-- A BIGINT identity PK, not a UUID, because the dispatcher reads in insertion
-- order and a monotonic integer is the cheapest way to get that. event_id is
-- the UUID that travels to Kafka and is what consumers dedupe on.

-- Essential: the table grows until the retention job prunes published rows, but
-- the dispatcher's query only ever scans unpublished ones - normally a handful.
CREATE INDEX idx_outbox_unpublished ON outbox (id) WHERE published_at IS NULL;
