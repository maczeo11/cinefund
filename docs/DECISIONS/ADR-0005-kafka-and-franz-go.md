# ADR-0005: Kafka as the event bus, franz-go as the client

**Status:** accepted
**Date:** 2026-08-02

> **Status note (2026-08-06).** The "change-stream watcher" in the context below
> no longer exists — MongoDB was dropped ([ADR-0010](ADR-0010-postgres-only.md))
> and the outbox is the sole producer. The choice of Kafka and franz-go is
> unaffected.

## Context

The outbox and the change-stream watcher need somewhere to publish. Consumers
need durable, ordered, replayable delivery — a transcode request that is dropped
means a film that never becomes watchable, and a receipt email that is dropped
means a backer who thinks their money vanished.

Requirements:

- Durable — survives broker restart.
- Ordered per aggregate — `pledge.created` before `pledge.captured` for the same
  pledge.
- Replayable — reprocess after fixing a consumer bug.
- Multiple independent consumer groups over the same stream.
- Observable lag.

## Options considered

**Redis Streams.** Already running Redis, so no new infrastructure. Consumer
groups exist. But durability depends on Redis persistence configuration, retention
is memory-bound, and Redis is already load-bearing for cache, locks and rate
limits — coupling event delivery to it means one Redis incident degrades
everything at once.

**NATS JetStream.** Lighter than Kafka, genuinely good, simpler to operate. Fewer
people know it, and the ecosystem (tooling, UIs, hiring-manager recognition) is
smaller.

**Postgres as a queue** (`SKIP LOCKED` on a jobs table). No new infrastructure at
all, and honestly sufficient at this scale. But no independent consumer groups
over the same stream without building fan-out yourself, no replay after deletion,
and it puts long-running transcode dispatch load on the same database that
handles money.

**RabbitMQ.** Excellent routing, mature. But it's a queue, not a log: messages are
consumed and gone. Replay and multiple independent readers of the same history
are exactly what's wanted here.

**Kafka.**

## Decision

**Kafka in KRaft mode** (no ZooKeeper). One topic per aggregate, keyed by
aggregate id so ordering holds per entity. Retry topics plus a DLQ per consumer
group.

**Client: `github.com/twmb/franz-go`.**

- Pure Go, no cgo — cross-compilation and `FROM scratch`-ish images stay simple.
- `confluent-kafka-go` requires cgo and librdkafka, which complicates Docker
  builds and static linking.
- `sarama` is widely used, but its consumer-group API is comparatively easy to
  misuse (manual session/claim lifecycle, offset-commit footguns).
- franz-go is actively maintained and its API makes correct offset handling the
  default path.

## Consequences

**Good**

- Durable, replayable, partition-ordered delivery.
- Independent consumer groups: the projector, notifier, refund processor and
  analytics all read the same stream without interfering.
- Consumer lag is a first-class, easily-monitored metric.
- Retry topics and DLQ are a well-trodden pattern with clear semantics.
- Genuinely useful to know, and recognisable to interviewers.

**Bad**

- Real operational weight: a broker to run, disks to watch, ~1 GB of RAM at rest.
- The heaviest single component in local development. `docker compose up` is
  noticeably slower.
- Partition count is effectively permanent — increasing it later changes the
  key→partition mapping and breaks per-aggregate ordering for in-flight keys.
- Kafka has **no native delayed delivery**, so retry backoff must be built with
  tiered retry topics and consumer-side sleeping.
- Rebalancing semantics are a genuine learning curve, and the failure mode
  (a consumer that appears to do nothing) is confusing the first time.

**Commits us to**

- `AUTO_CREATE_TOPICS_ENABLE: false` and explicit topic declaration in
  `cmd/migrate`, so a typo'd topic name fails loudly rather than silently
  creating a topic nobody consumes.
- Choosing partition counts up front with growth in mind: 12 for `media` and
  `pledge`, 6 for `campaign` and `notification`, 3 for `user`.
- Every consumer being idempotent, because delivery is at-least-once.
- Alerting on any DLQ message. A DLQ nobody watches is a data-loss queue with
  extra steps.
