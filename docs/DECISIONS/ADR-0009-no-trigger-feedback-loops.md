# ADR-0009: No component is triggered by a store it writes to

**Status:** accepted — the *principle* stands; the change-stream mechanism described here is obsolete
**Date:** 2026-08-02
**Found:** during design review, before any code was written

> **Status note (2026-08-06).** The media pipeline's trigger here — a Mongo
> change stream on `media_assets` — was removed with MongoDB
> ([ADR-0010](ADR-0010-postgres-only.md)). The rule this ADR argues for still
> holds and is now satisfied trivially: every component publishes to the outbox
> and is triggered by Kafka, never by a store it wrote to.

## Context

`media_assets` is the Mongo collection holding one document per uploaded file.
`cmd/mediawatcher` opens a **change stream** on it — Mongo's "tell me when a
document changes" feature — and turns each matching change into a Kafka message
so a transcoder can pick up the work.

The original filter woke the watcher on three statuses:

```go
{"fullDocument.status", bson.D{{"$in", bson.A{"UPLOADED", "READY", "FAILED"}}}}
```

That looks reasonable until you ask **who writes each one**:

| Status | Written by |
| --- | --- |
| `UPLOADED` | the API, when the client confirms the upload |
| `READY` | the **transcoder**, when it finishes |
| `FAILED` | the **transcoder**, when it gives up |

Two of the three are written by a component that is *downstream of the watcher*.

## The bug

### Symptom — duplicate events

The transcoder already publishes `media.transcode.completed` to Kafka itself
([09 §1](../09-MEDIA-PIPELINE.md#1-the-pipeline)). So on completion:

```
transcoder writes status = READY to Mongo
transcoder publishes media.transcode.completed  → Kafka
change stream fires (READY was in the filter)
watcher publishes a second event                → Kafka
```

One thing happened, two events. Consumers are idempotent, so nothing visibly
breaks — which is exactly why this would have survived to production.

### The real problem — a latent infinite loop

Today no consumer of `READY` writes back to `media_assets`. The moment one does —
and a consumer that stamps `catalog_indexed_at` once a film goes live is an
entirely reasonable thing to add — the cycle closes:

```
transcoder writes READY
   → change stream fires
   → watcher publishes
   → consumer writes catalog_indexed_at to media_assets
   → document changed, status is still READY, filter still matches
   → change stream fires
   → watcher publishes
   → consumer writes
   → …
```

It runs until someone notices the disk filling.

#### Why the consumer keeps writing

Two things, and both are worth being explicit about because the loop looks
avoidable until you see them.

**The consumer is a reflex, not a decision.** Its whole body is:

```go
func handle(event) {
    media_assets.update({_id: event.AssetID},
                        {$set: {catalog_indexed_at: time.Now()}})
}
```

It has no memory of having run before, and no way to tell that the message it
just received was produced by its own previous write. Every message is
indistinguishable from the first.

**Every write is a genuinely different document**, so every write produces a real
oplog entry:

```
10:00:00.000   catalog_indexed_at = 10:00:00.000   → change → event
10:00:00.147   catalog_indexed_at = 10:00:00.147   → change → event
10:00:00.293   catalog_indexed_at = 10:00:00.293   → change → event
```

A constant value would self-terminate — Mongo writes no oplog entry for an update
with `modifiedCount: 0`, so the second iteration produces no event. But real code
writes `updated_at`, `last_seen_at`, `processed_at`, a `$inc` counter, a UUID.
**The self-terminating case is the rare one.**

#### The existing safeguards do not catch it

`processed_events` dedupes by `event_id`, and the watcher mints a fresh UUID per
publish:

```
event_id 4f2a…  "asset abc is READY"
event_id 9c81…  "asset abc is READY"
event_id e034…  "asset abc is READY"
```

These are not duplicates of one event — they are distinct events, each
legitimately caused by the previous one. Idempotent consumers, Kafka keying, and
the dedupe table all see this as correct behaviour and let it through.

That is the reason the fix must remove the cycle rather than filter it: every
mechanism normally relied on to catch repeated work is blind to this.

This is structurally identical to the well-known AWS failure where an S3 event
notification triggers a Lambda that writes back to the same bucket. Only the
transport differs:

| S3 / Lambda | CineFund |
| --- | --- |
| S3 bucket | `media_assets` collection |
| S3 event notification | Mongo change stream |
| Lambda function | `mediawatcher` + its consumers |

A microphone next to its own speaker. The feedback does not depend on the
technology.

## Options considered

**Filter out the loop.** Add `catalog_indexed_at` to an exclusion list, or match
only on specific `updateDescription.updatedFields`. Works, but it is a
configuration that must be kept correct forever — every future field added to the
document is a chance to reintroduce the loop, silently.

**Detect and break the cycle.** Track a hop count or a "written by" marker on the
document. More moving parts, and it makes the loop survivable rather than
impossible.

**Remove the cycle by construction.** Ensure the watcher can never observe a
write made by anything downstream of it.

## Decision

**The watcher matches `status == "UPLOADED"` and nothing else.**

`UPLOADED` is written by exactly one component — the API — and by nothing
downstream of the watcher. No future consumer will write it, because it is not a
status any downstream component has reason to set.

**The transcoder publishes its own terminal events directly to Kafka.** It is
already a Kafka producer, so it never needed the watcher as a bridge.

| Transition | Written by | Published by | Mechanism |
| --- | --- | --- | --- |
| `PENDING_UPLOAD → UPLOADED` | API | `mediawatcher` | change stream |
| `→ PROBING / QUEUED / TRANSCODING` | transcoder | — | internal, not an event |
| `→ READY` | transcoder | **transcoder** | direct Kafka produce |
| `→ FAILED` / `REJECTED` | transcoder | **transcoder** | direct Kafka produce |

The watcher exists for one reason: to bridge *"the API wrote to Mongo"* into
Kafka, because the API is not otherwise a producer on this path.

### The general rule

> **A component must never be triggered by a store it also writes to** — directly
> or through anything it triggers.

If that is unavoidable, the trigger filter must key on something only the
*upstream* writer produces. A filter is a weaker guarantee than an absent cycle.

## Consequences

**Good**

- The loop is impossible by construction, not prevented by configuration.
- No duplicate events on transcode completion.
- Narrower filter: less to reason about, fewer change-stream events processed.
- Adding a consumer that writes to `media_assets` is now safe by default. Nobody
  has to remember this ADR to avoid the trap.

**Bad**

- Two publishers on the media path (watcher and transcoder) rather than one.
  Slightly less uniform, and both need monitoring.
- The transcoder's terminal event is published *after* its Mongo write, so a
  crash between the two loses the event. Recovered by the same catch-up scan that
  already covers a missed `UPLOADED` ([08 §6](../08-EVENTING-OUTBOX-KAFKA.md#6-the-mongo-change-stream-watcher)).

**Related**

`transcode_jobs` is already a separate collection from `media_assets` for a
neighbouring reason: progress heartbeats fire every few seconds and would flood a
change stream that cares about a single transition. Same principle, applied to
volume rather than cycles.

CineFund uses **no storage event notifications at all** — no S3 events, no
Lambda. The pipeline is triggered by database state, never by an object write, so
the original S3-shaped version of this loop cannot occur regardless of bucket
layout. See [10 §1.1](../10-OBJECT-STORAGE.md#11-there-are-no-storage-event-triggers).

## Test

```
[ ] the watcher's $match rejects a document whose status is READY
[ ] a transcoder completion produces exactly ONE media.transcode.completed event
[ ] a consumer that writes to media_assets does not re-trigger the watcher
```

The third one is the regression test. Write it as a consumer that stamps a field
on the asset, then assert the watcher's publish count stays at 1.
