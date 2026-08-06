# 08 — Eventing: Outbox, Change Streams, Kafka

Two producers, one bus, many consumers. This is the spine of the system.

---

## 1. The dual-write problem, stated precisely

A pledge is captured. Two things must happen:

1. Postgres records the capture.
2. Downstream systems learn about it — receipt email, catalog update, analytics.

If you do them as two independent operations, there is no ordering that is safe:

```
COMMIT then PUBLISH:   process dies in between → DB is right, nobody was told.  Lost event.
PUBLISH then COMMIT:   transaction rolls back  → everyone was told about a thing that
                                                 didn't happen.  Phantom event.
```

Distributed transactions across Postgres and Kafka (XA/2PC) are theoretically
available and practically a bad idea: they need a transaction coordinator,
they hold locks across network calls, and Kafka's support for them is
transactional-producer semantics that still don't span your database.

**The outbox pattern removes the second write entirely.** The event is inserted
into a Postgres table *in the same transaction* as the domain change, so there is
only ever one write. A separate process moves rows from that table to Kafka.

The guarantee is **at-least-once**: if the dispatcher crashes after producing but
before marking the row published, it republishes. Consumers must be idempotent.
Exactly-once is not achievable here and pretending otherwise leads to worse bugs
than accepting duplicates.

---

## 2. Two mechanisms, deliberately

| Source | Mechanism | Why |
| --- | --- | --- |
| **Postgres** (money, campaigns) | outbox table + dispatcher | Postgres has no built-in CDC without Debezium, and the outbox row must commit atomically with the domain write |
| **Mongo** (media pipeline) | change stream on `media_assets` | Mongo has CDC built in; the media pipeline has no transaction to be atomic with — a status flip *is* the event |

This is not inconsistency for its own sake. Ask "what is this event atomic
with?" — for a pledge capture, the answer is "four other table writes", so it
needs an outbox. For a media asset reaching `UPLOADED`, the answer is "nothing",
so a change stream is strictly simpler and has zero write amplification.

Full reasoning in [ADR-0004](DECISIONS/ADR-0004-outbox-vs-change-streams.md).

---

## 3. Event envelope

Every message on every topic has this shape. Freeze it before writing the first
consumer — changing an envelope after five consumers exist is exactly the pain
your PRAJNA EventBridge contracts were designed to avoid.

```jsonc
{
  "event_id":       "0192f8a1-...",       // UUIDv7. Consumer dedupe key.
  "event_type":     "pledge.captured",    // "<aggregate>.<past-tense-verb>"
  "event_version":  1,                    // bump on breaking payload change
  "occurred_at":    "2026-08-02T11:04:22.481Z",
  "aggregate_type": "pledge",
  "aggregate_id":   "0192k...",           // == Kafka message key
  "trace_id":       "00-4bf92f35...-01",  // W3C traceparent
  "producer":       "api@v0.4.1",
  "payload":        { }                    // versioned, type-specific
}
```

Rules that make this survivable:

- **`aggregate_id` is the Kafka key.** All events for one pledge land on one
  partition, so they are consumed in order. Cross-aggregate ordering is not
  guaranteed and no consumer may assume it.
- **Payloads are additive-only within a version.** Adding an optional field is
  fine. Removing a field, renaming one, or changing a type means `v2` — a new
  event type suffix (`pledge.captured.v2`) or a new topic. Consumers ignore
  unknown fields.
- **Payloads carry ids and the minimum data, not whole entities.** A consumer
  that needs the full campaign reads it. Fat events go stale between production
  and consumption, and they leak schema.
- **`occurred_at` is when the domain event happened**, not when it was published.
  The gap between the two is dispatcher lag and you want to be able to measure it.

---

## 4. Topics

Naming: `cinefund.<aggregate>.v<major>`. One topic per aggregate, event types
multiplexed inside — not a topic per event type, which produces topic sprawl and
breaks per-aggregate ordering.

| Topic | Partitions | Retention | Key | Event types |
| --- | --- | --- | --- | --- |
| `cinefund.campaign.v1` | 6 | 7 d | campaign_id | `campaign.published`, `campaign.updated`, `campaign.funded`, `campaign.failed`, `campaign.cancelled`, `campaign.released` |
| `cinefund.pledge.v1` | 12 | 30 d | pledge_id | `pledge.created`, `pledge.captured`, `pledge.failed`, `refund.requested`, `refund.completed` |
| `cinefund.media.v1` | 12 | 7 d | asset_id | `media.upload.completed`, `media.probe.completed`, `media.transcode.requested`, `media.transcode.completed`, `media.transcode.failed` |
| `cinefund.user.v1` | 3 | 7 d | user_id | `user.registered`, `user.updated`, `creator.approved` |
| `cinefund.notification.v1` | 6 | 3 d | user_id | `notification.requested` |

Plus, per consumer group that needs them:

| Topic | Purpose |
| --- | --- |
| `cinefund.<x>.v1.retry.30s` | first retry tier |
| `cinefund.<x>.v1.retry.5m` | second retry tier |
| `cinefund.<x>.v1.dlq` | terminal failures, retained 30 days |

**Pledge retention is 30 days, not 7.** Money events are the ones you'll want to
replay when reconstructing what happened during an incident, and 7 days is
shorter than the time it takes for a discrepancy to be noticed.

### Partition count reasoning

Partitions cap consumer parallelism — you cannot have more useful consumers in a
group than partitions. 12 on `media` because transcoding is the slow consumer you
will scale out. 6 on `campaign` because campaign events are low-volume. 3 on
`user` because it's almost idle. Over-partitioning costs open file handles and
rebalance time; under-partitioning caps your throughput permanently, because
increasing partitions later **changes the key→partition mapping** and breaks
per-aggregate ordering for in-flight keys. Err slightly high.

---

## 5. The outbox dispatcher

`cmd/dispatcher`. Under 200 lines. One of the highest-value-per-line components
in the system.

```go
func (d *Dispatcher) tick(ctx context.Context) (int, error) {
    tx, err := d.pg.Begin(ctx)
    if err != nil { return 0, err }
    defer tx.Rollback(ctx)

    rows, err := tx.Query(ctx, `
        SELECT id, event_id, event_type, event_version, aggregate_type,
               aggregate_id, payload, trace_id, created_at
          FROM outbox
         WHERE published_at IS NULL
         ORDER BY id
           FOR UPDATE SKIP LOCKED
         LIMIT $1`, d.batchSize)
    if err != nil { return 0, err }

    batch := scan(rows)
    if len(batch) == 0 { return 0, nil }

    records := make([]*kgo.Record, len(batch))
    for i, e := range batch {
        records[i] = &kgo.Record{
            Topic:   topicFor(e.AggregateType),
            Key:     []byte(e.AggregateID),          // ordering per aggregate
            Value:   encodeEnvelope(e),
            Headers: []kgo.RecordHeader{{Key: "traceparent", Value: []byte(e.TraceID)}},
        }
    }

    // Synchronous produce: every record acked by all in-sync replicas before we
    // mark anything published. acks=all, idempotent producer enabled.
    if err := d.kafka.ProduceSync(ctx, records...).FirstErr(); err != nil {
        _ = d.recordFailure(ctx, batch, err)   // separate tx: attempts++, last_error
        return 0, err
    }

    if _, err := tx.Exec(ctx,
        `UPDATE outbox SET published_at = now() WHERE id = ANY($1)`, ids(batch)); err != nil {
        return 0, err     // rollback → rows stay unpublished → republished → duplicates.
                          // Acceptable: consumers are idempotent.
    }
    return len(batch), tx.Commit(ctx)
}
```

### Why each piece is the way it is

| Choice | Reason |
| --- | --- |
| `FOR UPDATE SKIP LOCKED` | Run N dispatcher replicas with zero coordination. Each claims a disjoint batch instead of blocking. No leader election needed. |
| `ORDER BY id` | Preserves per-aggregate ordering, since events for one aggregate are inserted in order and Kafka keys them to one partition. |
| Batch of 100 | Amortises the produce round-trip. Larger batches mean a longer transaction holding more row locks. 100 is a good default; make it configurable. |
| `ProduceSync` with `acks=all` | Marking a row published when the broker hasn't durably accepted it is exactly the loss the outbox exists to prevent. |
| Idempotent producer | Kafka-level dedupe on the broker for retries within a producer session. Free; enable it. |
| Rollback → duplicates | The trade the whole design accepts. Duplicates are handled by consumers; loss is not recoverable. |
| Poll interval 200 ms | Latency floor. Add `LISTEN/NOTIFY` from an `AFTER INSERT` trigger to wake the loop immediately, and keep the poll as the safety net — notifications are not durable. |

### Retention

Published rows are pruned by the scheduler:

```sql
DELETE FROM outbox WHERE published_at < now() - interval '7 days';
```

Keep 7 days so you can answer "was this event published, and when?" during an
incident. Deleting immediately after publish makes the table small and the
debugging impossible.

### Health

`outbox_lag_seconds = now() − min(created_at) WHERE published_at IS NULL`.
Alert above 30 s. This single metric catches: dispatcher crashed, Kafka down,
broker full, a poison event failing repeatedly.

---

## 6. The Mongo change stream watcher

`cmd/mediawatcher`. Exactly one logical reader.

```go
pipeline := mongo.Pipeline{{{"$match", bson.D{
    {"operationType", bson.D{{"$in", bson.A{"insert", "update", "replace"}}}},
    // ONLY the transition the API causes. Never a status the transcoder writes.
    // See §6.1 — the watcher must not observe its own downstream's writes.
    {"fullDocument.status", "UPLOADED"},
}}}}

opts := options.ChangeStream().
    SetFullDocument(options.UpdateLookup).
    SetResumeAfter(savedToken)          // nil on first ever start

stream, err := assets.Watch(ctx, pipeline, opts)
for stream.Next(ctx) {
    var ev ChangeEvent
    if err := stream.Decode(&ev); err != nil { continue }

    if err := w.publish(ctx, ev); err != nil {
        // Do NOT advance the token. Retry; a duplicate publish is fine, a skip is not.
        w.log.Error("publish failed", "asset_id", ev.FullDocument.ID, "error", err)
        continue
    }
    w.saveResumeToken(ctx, stream.ResumeToken())   // AFTER a successful publish
}
```

### 6.1 The feedback-loop rule

**A component must never be triggered by a store it also writes to.**
Full context, including the bug this prevents, in
[ADR-0009](DECISIONS/ADR-0009-no-trigger-feedback-loops.md).

The transcoder writes `media_assets` (`PROBING`, `QUEUED`, `TRANSCODING`,
`READY`, `FAILED`). The watcher watches `media_assets`. If the watcher's filter
included any of those statuses, every transcoder write would produce a Kafka
event, and any consumer that wrote back to the asset would close the loop:

```
write → change stream → Kafka → consumer → write → change stream → …
```

It runs until someone notices the disk filling. This is the same failure as the
classic S3-event-triggers-Lambda-that-writes-to-the-same-bucket loop — only the
transport differs.

So the boundary is drawn by **who publishes what**, not by filtering after the
fact:

| Transition | Written by | Published by | Mechanism |
| --- | --- | --- | --- |
| `PENDING_UPLOAD → UPLOADED` | **API** | `mediawatcher` | change stream |
| `UPLOADED → PROBING → QUEUED` | transcoder | — | internal, not an event |
| `→ TRANSCODING` | transcoder | — | progress goes to gRPC + Mongo, not Kafka |
| `→ READY` | transcoder | **transcoder itself** | direct Kafka produce |
| `→ FAILED` | transcoder | **transcoder itself** | direct Kafka produce |
| `→ REJECTED` | transcoder | **transcoder itself** | direct Kafka produce |

The watcher exists for exactly one reason: to bridge *"the API wrote to Mongo"*
into Kafka, because the API is not otherwise a producer on this path. The
transcoder **is already a Kafka producer**, so it needs no bridge — it publishes
its own terminal events directly and the watcher never sees them.

Two consequences worth stating:

- The watcher's `$match` is `status == "UPLOADED"` and nothing else. Narrower
  filter, less to reason about, no loop possible by construction.
- The transcoder's terminal events are published **after** the Mongo write
  succeeds, so a crash in between means no event — which the catch-up scan
  (§6, point 3) recovers, the same way it recovers a missed `UPLOADED`.

`transcode_jobs` being a separate collection from `media_assets` serves the same
principle: progress heartbeats fire every few seconds and would otherwise flood a
change stream that only cares about a single transition.

### The three things that will bite you

1. **`SetFullDocument(UpdateLookup)`.** Without it, an `update` event contains
   only the changed fields, so `fullDocument.status` in the `$match` is empty and
   nothing ever matches. This is the single most common change-stream mistake.
   Note the cost: `UpdateLookup` re-reads the document at *lookup* time, so it
   may reflect a *later* state than the event. Always re-read status defensively
   in the consumer rather than trusting the event to be a point-in-time snapshot.
2. **Resume token persistence.** Store it in a `watcher_state` collection,
   updated after each successful publish. On restart, `SetResumeAfter`. Without
   this, a restart either replays everything from the oplog start or skips
   whatever happened while you were down.
3. **Token invalidation.** If the watcher is down longer than the oplog window,
   the token is no longer in the oplog and `Watch` fails with
   `ChangeStreamHistoryLost`. Handle it explicitly: log CRITICAL, fall back to
   `SetStartAtOperationTime(now)`, and run a catch-up scan for assets stuck in
   `UPLOADED` for more than 5 minutes. **The catch-up scan is what makes this
   robust** — treat the change stream as the fast path and a periodic sweep as
   the guarantee.

### Why not run this inside `cmd/transcoder`?

Because a change stream needs exactly one reader per resume token, and you want
many transcoders. Merging them means either one transcoder (no scale) or N
readers duplicating every event (no correctness). Splitting the watcher out is
the whole reason it exists as a separate binary.

---

## 7. Consumers

### Standard consumer loop

```go
func (c *Consumer) Run(ctx context.Context) error {
    for {
        fetches := c.client.PollRecords(ctx, 100)
        if errs := fetches.Errors(); len(errs) > 0 { /* log; kgo retries */ }

        fetches.EachRecord(func(r *kgo.Record) {
            ctx := telemetry.ContextFromKafkaHeaders(ctx, r.Headers)
            var env Envelope
            if err := json.Unmarshal(r.Value, &env); err != nil {
                c.toDLQ(ctx, r, "malformed envelope")   // never retry unparseable
                return
            }

            if c.alreadyProcessed(ctx, env.EventID) { return }   // processed_events

            switch err := c.handle(ctx, env); {
            case err == nil:
                c.markProcessed(ctx, env.EventID)
            case errors.Is(err, ErrPermanent):
                c.toDLQ(ctx, r, err.Error())
            default:
                c.toRetry(ctx, r, env)                   // transient → retry topic
            }
        })
        c.client.CommitRecords(ctx, fetches.Records()...)
    }
}
```

**Commit offsets after processing, not before.** Committing first turns a crash
into a lost message. Committing after turns it into a duplicate, which the
dedupe table absorbs. Always prefer the duplicate.

### Idempotency

Three strategies, in order of preference:

1. **Naturally idempotent operation** — `UPDATE films SET status='RELEASED' WHERE _id=?`
   is safe any number of times. Prefer designing handlers this way; it needs no
   bookkeeping at all.
2. **Unique constraint** — inserting a `transcode_job` with
   `{asset_id, pipeline_version}` unique makes a duplicate delivery a no-op error.
3. **`processed_events` table** — for side effects that aren't idempotent, like
   sending an email. Insert `{consumer}:{event_id}`; duplicate key means skip.

Emails specifically: the dedupe record must be written **before** calling the
mail provider, and the provider call must be given its own idempotency key. The
alternative ordering sends a second email whenever the process dies between the
send and the record.

### Retries and DLQ

```
main topic ──fail──► retry.30s ──fail──► retry.5m ──fail──► dlq
                        │                    │
                    consumer sleeps      consumer sleeps
                    until r.Timestamp    until r.Timestamp
                    + 30s                + 5m
```

The delay is implemented by the retry-topic consumer checking the record
timestamp and sleeping (or pausing the partition) until the delay has elapsed.
Kafka has no native delayed delivery; this is the standard workaround.

**Transient vs permanent** is the classification that makes this work:

| Permanent → straight to DLQ | Transient → retry |
| --- | --- |
| malformed JSON | connection refused |
| unknown event type in a strict consumer | timeout |
| referenced entity doesn't exist and never will | 5xx from a downstream API |
| validation failure | Mongo/Postgres unavailable |
| FFmpeg says the file isn't a video | disk full, OOM-killed |

Retrying a permanent failure 3 times just delays the DLQ by 5 minutes and burns
CPU. Classify deliberately.

DLQ records keep the original headers plus `x-dlq-reason`, `x-dlq-original-topic`,
`x-dlq-attempts`. `POST /admin/dlq/{topic}/replay` re-produces them to the main
topic. **Alert on any DLQ message.** A DLQ nobody looks at is a data-loss queue
with extra steps.

---

## 8. Consumer groups

| Group | Topics | Does |
| --- | --- | --- |
| `catalog-projector` | campaign, media, user | maintains Mongo `campaign_pages` and `films` |
| `transcoder` | media | claims transcode jobs, runs FFmpeg |
| `notifier` | pledge, campaign, notification | emails |
| `refund-processor` | pledge | calls Razorpay Refunds API |
| `analytics` | all | daily rollups |
| `search-indexer` | campaign, media | text index refresh (folded into projector in v1) |

One group per concern, not one group per binary. `cmd/notifier` may host several
groups; separating them means a slow email provider doesn't stall the projector.

---

## 9. Local Kafka

KRaft mode — no ZooKeeper. Fewer moving parts, and ZooKeeper is deprecated.

```yaml
kafka:
  image: bitnami/kafka:3.7
  environment:
    KAFKA_CFG_NODE_ID: "0"
    KAFKA_CFG_PROCESS_ROLES: "controller,broker"
    KAFKA_CFG_CONTROLLER_QUORUM_VOTERS: "0@kafka:9093"
    KAFKA_CFG_LISTENERS: "PLAINTEXT://:9092,CONTROLLER://:9093"
    KAFKA_CFG_ADVERTISED_LISTENERS: "PLAINTEXT://kafka:9092"
    KAFKA_CFG_CONTROLLER_LISTENER_NAMES: "CONTROLLER"
    KAFKA_CFG_AUTO_CREATE_TOPICS_ENABLE: "false"   # topics are declared, not conjured
```

`AUTO_CREATE_TOPICS_ENABLE: false` is deliberate. Auto-creation means a typo in a
topic name silently produces to a new topic that nobody consumes, and you spend
an hour wondering why the consumer is idle. Create topics explicitly in
`cmd/migrate` or a `make topics` target with the right partition counts.

Client library: **`franz-go`** (`github.com/twmb/franz-go`). Pure Go, no cgo, no
librdkafka, actively maintained, and the API is pleasant. `confluent-kafka-go`
needs cgo and complicates cross-compilation and Docker builds; `sarama` is
widely used but its consumer-group API is harder to use correctly.

---

## 10. Tests

| # | Scenario | Assertion |
| --- | --- | --- |
| E1 | Insert domain row + outbox in one tx, kill process before dispatch | on restart the event is published exactly once |
| E2 | Kafka unavailable during dispatch | rows stay unpublished; `outbox_lag` grows; all delivered on recovery |
| E3 | Dispatcher crashes between produce and `UPDATE published_at` | event delivered twice; consumer dedupe makes it a single effect |
| E4 | Two dispatcher replicas, 1000 outbox rows | each row published exactly once (`SKIP LOCKED` proof) |
| E5 | Consumer receives the same event twice | one side effect |
| E6 | Consumer throws transient error 3× | lands in DLQ with correct headers |
| E7 | Consumer throws permanent error once | DLQ immediately, no retry topics touched |
| E8 | Watcher restarts | resumes from token, no gap, no full replay |
| E9 | Watcher down past the oplog window | detects `ChangeStreamHistoryLost`, catch-up scan finds the stuck assets |
| E10 | `traceparent` propagation | one trace spans HTTP → outbox → Kafka → transcoder |

E4 and E9 are the two that prove the design rather than the code. Write them.
