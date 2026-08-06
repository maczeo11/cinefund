# CineFund

Crowdfunding and streaming for short films. Creators launch campaigns for films
that don't exist yet, backers fund them, and funded films get transcoded and
streamed on the same platform.

## What's here right now

Work in progress. The money path is the first thing being built, because it's
the part where being wrong costs real money.

**Done**

- Migration runner + full Postgres schema (15 migrations: users, campaigns,
  reward tiers, pledges, payment events, double-entry ledger, refunds, payouts,
  entitlements, outbox, idempotency keys, audit log). All applied against a
  live Postgres.
- Pledge service with a **gateway interface** so tests never touch the network:
  order creation, webhook signature verification, idempotent capture handling
  (Redis `SETNX` guard in front of a Postgres unique constraint), a state
  machine, and ledger entries written in the same transaction as the state
  change.
- The whole pledge flow runs offline against a fake gateway and fake repo. The
  concurrency tests (50× the same webhook → exactly one capture) are part of
  the normal test run.
- API skeleton with liveness/readiness probes; boots against the real stack.

**Next**

- HTTP layer: `POST /webhooks/razorpay`, `POST /campaigns/{id}/pledges`
- Auth, campaigns, then the outbox → Kafka dispatcher
- See [16 — Build order](docs/16-BUILD-ORDER.md) for the full plan.

## Stack

| Concern | Choice |
| --- | --- |
| API | Go 1.26, Gin |
| Database | PostgreSQL 16 (`pgx/v5`, no ORM) — one datastore for everything |
| Cache / locks | Redis 7 |
| Event bus | Kafka (`franz-go`), fed by a transactional outbox |
| Object storage | MinIO locally, S3-compatible in prod |
| Transcoding | FFmpeg subprocess, HLS ABR |
| Payments | Razorpay Orders + Webhooks |
| Telemetry | `log/slog`, OpenTelemetry, Prometheus |

A word on the single datastore: this was originally Postgres + MongoDB (Mongo
for the catalog, Postgres for money). That was reversed before any code was
written — the outbox does the job change streams were brought in for, and one
store keeps every financial invariant declarative. Reasoning in
[ADR-0010](docs/DECISIONS/ADR-0010-postgres-only.md).

## Why this project exists

The interesting parts are the four places a naive implementation is quietly
wrong:

1. **A payment webhook arrives twice.** Razorpay retries on timeout; a naive
   `campaign.raised += amount` double-credits the campaign. Belt and braces
   here: a Redis `SETNX` guard in front of a Postgres unique constraint,
   because Redis can lose the key.
2. **The DB commits and the process dies before publishing.** The pledge is
   recorded but no receipt email is sent and the search index goes stale. A
   transactional outbox makes the domain write and the event insert one
   Postgres transaction.
3. **Transcoding a 4 GB film inside an HTTP handler.** Fixed by never letting
   bytes touch the API: presigned upload straight to object storage, then a
   pool of Go workers shelling out to FFmpeg.
4. **All-or-nothing funding.** Money is held, not spent. If a campaign misses
   its goal at the deadline, every pledge refunds — that needs a real
   double-entry ledger, not an integer column.

## Running it

```bash
cp .env.example .env
make up          # postgres, redis, kafka, minio in Docker
make migrate     # apply schema
make run-api     # :8080
```

The pledge tests don't need any of that:

```bash
go test ./internal/pledge/...
```

## Documents

The design was written up before the code; the docs are the reference the code
is checked against. [Start here](docs/00-PRODUCT-SPEC.md).

| # | Doc | Covers |
| --- | --- | --- |
| 00 | [Product spec](docs/00-PRODUCT-SPEC.md) | actors, funding rules, state machines, glossary |
| 01 | [Architecture](docs/01-ARCHITECTURE.md) | components, request lifecycle, failure modes |
| 02 | [Postgres data model](docs/02-DATA-MODEL-POSTGRES.md) | tables, indexes, constraints, invariants |
| 04 | [API spec](docs/04-API-SPEC.md) | endpoints, errors, status codes |
| 05 | [Auth & security](docs/05-AUTH-SECURITY.md) | JWT rotation, RBAC, threat model |
| 06 | [Payments (Razorpay)](docs/06-PAYMENTS-RAZORPAY.md) | order creation, webhook verification, idempotency, refunds |
| 07 | [Ledger](docs/07-LEDGER.md) | accounts, money movements, reconciliation |
| 08 | [Eventing](docs/08-EVENTING-OUTBOX-KAFKA.md) | outbox, topics, retries, DLQ |
| 09 | [Media pipeline](docs/09-MEDIA-PIPELINE.md) | upload → probe → transcode → HLS |
| 10 | [Object storage](docs/10-OBJECT-STORAGE.md) | buckets, keys, presigning |
| 11 | [Caching](docs/11-CACHING-REDIS.md) | key scheme, TTLs, invalidation |
| 12 | [Rate limiting](docs/12-RATE-LIMITING.md) | token buckets, headers |
| 13 | [gRPC control plane](docs/13-GRPC-CONTROL-PLANE.md) | worker contracts, progress |
| 14 | [Observability](docs/14-OBSERVABILITY.md) | logs, metrics, traces, SLOs |
| 15 | [Project layout](docs/15-PROJECT-LAYOUT.md) | file tree, package ownership |
| 16 | [Build order](docs/16-BUILD-ORDER.md) | phases, acceptance criteria, calendar |
| 17 | [Testing strategy](docs/17-TESTING-STRATEGY.md) | unit vs integration split |
| 18 | [Local dev & deploy](docs/18-LOCAL-DEV-DEPLOY.md) | compose, env, runbook |
| 19 | [Performance](docs/19-PERFORMANCE.md) | load tests, capacity, bottlenecks |
| — | [Decision records](docs/DECISIONS/README.md) | why contested choices went the way they did |
| — | [Devlog](docs/DEVLOG.md) | running notes: what broke, what was tried |

## License

Apache-2.0.
