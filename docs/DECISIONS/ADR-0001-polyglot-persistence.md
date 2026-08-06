# ADR-0001: Postgres for money, Mongo for catalog

**Status:** superseded by [ADR-0010](ADR-0010-postgres-only.md) on 2026-08-06
**Date:** 2026-08-02

> **Superseded.** The split was reversed before any code was written — see
> [ADR-0010](ADR-0010-postgres-only.md). The reasoning below is left intact
> because it is still the right way to think about the trade-off; what changed
> is the weight given to solo-build calendar cost, and the realisation that the
> outbox already does the job the change streams were brought in for.

## Context

CineFund has two kinds of data with genuinely different requirements.

**Money** — pledges, payments, refunds, ledger entries, entitlements. A pledge
capture writes five rows across four tables and must be atomic. A limited reward
tier must not oversell under concurrency. A refund must not be issued twice.
These are correctness requirements where the cost of being wrong is real money
and a support incident.

**Catalog** — film metadata, media assets, renditions, transcode jobs, comments.
Read-heavy, denormalised, shape-shifting (a rendition list differs per asset;
probe output differs per codec). Nothing here needs a transaction, and the media
pipeline wants change-data-capture.

The prior project (MagicStream) used MongoDB throughout, so there's an argument
from familiarity for continuing.

## Options considered

**MongoDB only.** Familiar, one datastore to operate. But multi-document
transactions require a replica set even for local development, financial
invariants (tier limits, single-active-refund, ledger balance) must be enforced
in Go rather than declaratively, and there is no equivalent of
`FOR UPDATE SKIP LOCKED` for the outbox dispatcher. Every money bug becomes an
application bug rather than an impossible state.

**Postgres only.** Correct for money and perfectly capable of the catalog via
`JSONB`. Costs: no change streams, so the media pipeline needs polling or
Debezium; text search is weaker than Mongo's weighted text indexes at this scale
without adding an extension; and the rendition/probe documents are genuinely
document-shaped.

**Both.** Each store does what it's good at.

## Decision

**Postgres owns anything where being wrong costs money.** Users, creator
profiles, campaigns, reward tiers, pledges, payments, ledger, refunds, payouts,
entitlements, outbox, idempotency keys, audit log.

**Mongo owns read-heavy, shape-shifting data.** Film catalog, campaign page
projections, media assets, transcode jobs, campaign updates, comments, analytics
rollups.

They are never written in the same transaction. Mongo is populated from Kafka
events originating in the Postgres outbox, making the catalog an **eventually
consistent read model** with a bounded lag (alert at 30 s, typically < 500 ms).

Where staleness is unacceptable — a backer checking whether their pledge
registered — the read goes to Postgres directly, even on a page otherwise served
from Mongo.

## Consequences

**Good**

- Financial invariants are enforced by `CHECK`, `UNIQUE` and deferred constraint
  triggers. Double-crediting requires two independent constraints to fail at once.
- The outbox dispatcher gets `FOR UPDATE SKIP LOCKED`, which makes N replicas
  work with zero coordination.
- The media pipeline gets change streams for free.
- Weighted text search on the catalog without extra infrastructure.

**Bad**

- Two datastores to run, back up, monitor and restore-test. Real ops cost.
- Two drivers, two connection pools, two failure modes in every readiness check.
- Cross-store references (`campaigns.pitch_asset_id`, `entitlements.film_id`) have
  no referential integrity. The application enforces them and they are documented
  as such in [02](../02-DATA-MODEL-POSTGRES.md).
- A new class of bug: read-model divergence. Mitigated by reconciliation check R2
  and by reading Postgres directly wherever it matters.
- Learning cost — the existing Postgres experience here is DynamoDB-shaped, which
  is a different mental model.

**Commits us to**

- Every cross-store consistency question is answered with "eventually, via
  Kafka", never with a distributed transaction.
- A projector consumer that must be idempotent and must be monitored.
