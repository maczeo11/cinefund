# ADR-0004: Outbox for Postgres, change streams for Mongo

**Status:** partially superseded by [ADR-0010](ADR-0010-postgres-only.md) on 2026-08-06

> **The change-stream half no longer applies.** With Mongo removed, the outbox
> is the *only* event mechanism: media status transitions publish through it in
> the same transaction as the status change. The reasoning below about why
> Postgres needs an outbox at all is unchanged and still load-bearing.
**Date:** 2026-08-02

## Context

Domain changes in both datastores must reach Kafka. The dual-write problem
applies: committing to the database and publishing to Kafka as two independent
operations loses events (crash between commit and publish) or invents them
(publish succeeds, transaction rolls back).

But the two stores present different problems.

A **pledge capture** in Postgres writes five rows across four tables. The event
must be atomic *with those writes* — publishing an event for a transaction that
rolled back is exactly as bad as losing one.

A **media asset reaching `UPLOADED`** in Mongo is a single-document update. There
is nothing for the event to be atomic *with*. The status change **is** the event.

## Options considered

**Outbox for both.** Uniform. But it means the media pipeline writes an outbox
document on every status change, plus a dispatcher polling Mongo — reimplementing
by hand what change streams already provide, with more write amplification.

**Change streams for both.** Impossible: Postgres has no built-in CDC. It needs
Debezium, which is a Kafka Connect cluster to deploy, configure and operate, plus
logical replication slots that will silently fill your WAL if a consumer stalls.
That is a lot of infrastructure for a solo project.

**Publish directly, accept the risk.** Fine until the first crash between commit
and publish, at which point a backer's receipt is never sent and the catalog is
permanently stale with no way to detect it.

**Mechanism per store, chosen by what the event must be atomic with.**

## Decision

**Postgres → transactional outbox.** `INSERT INTO outbox` in the same transaction
as the domain write. `cmd/dispatcher` claims batches with
`SELECT ... FOR UPDATE SKIP LOCKED`, produces synchronously with `acks=all`, then
marks rows published.

**Mongo → change stream.** `cmd/mediawatcher` watches `media_assets` filtered to
the statuses that matter, publishes to Kafka, and persists the resume token
**after** a successful publish.

The decision rule, stated so it generalises: **ask what the event must be atomic
with.** If the answer is "other writes", you need an outbox. If the answer is
"nothing", a change stream is strictly simpler.

## Consequences

**Good**

- Money events are never lost and never phantom — the guarantee that matters most.
- The media pipeline gets CDC with no extra write path and no dispatcher.
- Two well-known patterns implemented properly, rather than one pattern stretched
  over both.
- N dispatcher replicas need zero coordination thanks to `SKIP LOCKED`.

**Bad**

- Two mechanisms to understand, monitor and debug. `outbox_lag_seconds` and the
  watcher's resume-token age are separate health signals.
- The outbox table grows and needs pruning (published rows older than 7 days).
- The watcher is a **singleton** — one logical reader per resume token. It cannot
  be scaled horizontally, so it is a single point of failure for the media
  pipeline.
- Change streams require a Mongo **replica set**, even for a single node locally.
  Forgetting `--replSet rs0` produces `The $changeStream stage is only supported
  on replica sets`, which is a confusing first-run error.
- If the watcher is down longer than the oplog window, the resume token is gone
  and events are silently skipped.

**Commits us to**

- **At-least-once, never exactly-once.** The dispatcher can crash between
  producing and marking published, so consumers must be idempotent. This is
  accepted, not worked around.
- A **catch-up scan** as the guarantee behind the change stream: a periodic sweep
  for assets stuck in `UPLOADED` for more than 5 minutes. The change stream is the
  fast path; the sweep is what makes it robust. Without it, `ChangeStreamHistoryLost`
  means permanently stuck assets.
- Monitoring both lag signals, because a silent stall in either is invisible from
  the API.
