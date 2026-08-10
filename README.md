# CineFund

Crowdfunding platform for short films. Creators launch all-or-nothing campaigns,
backers fund them through Razorpay, and funded films get transcoded into adaptive
bitrate HLS for streaming.

Built with Go, PostgreSQL, Redis, Kafka, MinIO, and FFmpeg.

## How it works

The system runs as five separate binaries sharing a common `internal/` codebase:

- **api** — HTTP server handling campaigns, pledges, uploads, and payment webhooks
- **dispatcher** — polls an outbox table in Postgres and publishes events to Kafka
- **transcoder** — consumes Kafka events and runs FFmpeg to produce HLS streams
- **migrate** — applies embedded SQL migrations (no external tool dependencies)
- **seed** — inserts deterministic test data for local development

```
                          ┌──────────────┐
                          │   Browser    │
                          └──────┬───────┘
                                 │
                    ┌────────────▼────────────┐
                    │    API Server (Gin)      │
                    │                         │
                    │  campaigns, pledges,    │
                    │  uploads, webhooks      │
                    └───┬────────┬────────┬───┘
                        │        │        │
               ┌────────▼──┐ ┌──▼────┐ ┌─▼──────────┐
               │ PostgreSQL │ │ Redis │ │  Razorpay  │
               │            │ └───────┘ └────────────┘
               │  + outbox  │
               └────┬───────┘
                    │ poll (SKIP LOCKED)
               ┌────▼───────┐
               │ Dispatcher  │──────► Kafka
               └─────────────┘          │
                                   ┌────▼───────┐
                                   │ Transcoder  │
                                   │  (FFmpeg)   │
                                   └────┬────────┘
                                        │
                                   ┌────▼────┐
                                   │ MinIO/S3 │◄─── Browser (presigned uploads)
                                   └──────────┘
```

## What's interesting about it

**Payments aren't just Stripe-checkout-and-done.** When Razorpay sends a webhook,
the system does HMAC signature verification, then runs a two-layer idempotency
check — Redis SETNX as a fast path, Postgres unique constraint as the durable
fallback. Inside a single transaction it updates the pledge, increments the
campaign total, bumps the tier count, and writes balanced double-entry ledger
entries. If the DB write fails, it releases the Redis lock so retries aren't
blocked.

**Money is tracked as a double-entry ledger.** Not just `balance += amount`.
Every pledge capture produces matching DEBIT and CREDIT entries across escrow
accounts. Postgres enforces the invariant that debits equal credits using a
deferred constraint trigger at commit time. All amounts are in paise (integers)
to avoid floating-point issues.

**Video goes through a real transcode pipeline.** Files upload directly to S3
via presigned URLs — bytes never pass through the API server. The transcoder
probes the file with ffprobe (codec validation, duration limits, resolution
checks with rotation handling), generates an ABR ladder down from source
resolution (never upscales), and encodes with strict GOP alignment — 24fps,
48-frame keyframe interval, scene detection disabled — so HLS quality switching
works without glitches. The master playlist uploads last as an atomic commit.

**Workers handle crashes.** Transcode jobs are claimed via `SELECT FOR UPDATE
SKIP LOCKED` with a lease timeout. Workers heartbeat to extend their lease. If a
worker dies, another picks up the job. Fencing tokens prevent the dead worker
from writing stale results if it wakes back up.

**Domain events don't get lost.** State changes and outbox rows insert in the
same Postgres transaction. A separate dispatcher binary polls unpublished rows
and pushes them to Kafka. If the API crashes after commit, the events survive
in the outbox.

## Running locally

You need Go 1.26+, Docker, and FFmpeg (for the transcoder).

```bash
cp .env.example .env
make up              # postgres, redis, kafka, minio
make migrate         # apply schema
make seed            # sample data (optional)

# each in its own terminal
make run-api
make run-dispatcher
make run-transcoder
```

## Make targets

| Command            | What it does                                  |
|--------------------|-----------------------------------------------|
| `make up`          | Start infra containers                        |
| `make down`        | Stop containers                               |
| `make nuke`        | Stop + delete volumes                         |
| `make logs`        | Tail container logs                           |
| `make migrate`     | Run migrations forward                        |
| `make migrate-down`| Roll back one migration                       |
| `make seed`        | Insert dev data                               |
| `make run-api`     | Start API on :8080                            |
| `make run-dispatcher` | Start outbox publisher                     |
| `make run-transcoder` | Start transcode workers                    |
| `make test`        | Unit tests                                    |
| `make test-race`   | Unit tests with race detector (runs in Docker)|
| `make test-int`    | Integration tests (needs Docker services)     |
| `make lint`        | golangci-lint                                 |

## API

| Method | Path | Description |
|--------|------|-------------|
| GET    | /health/live | Liveness check |
| GET    | /health/ready | Readiness check (postgres + redis) |
| GET    | /api/v1/campaigns | List campaigns |
| POST   | /api/v1/campaigns | Create campaign (DRAFT) |
| GET    | /api/v1/campaigns/:id | Get campaign |
| POST   | /api/v1/campaigns/:id/publish | Set campaign LIVE |
| POST   | /api/v1/campaigns/:id/tiers | Add reward tier |
| POST   | /api/v1/campaigns/:id/pledges | Create pledge + payment order |
| POST   | /api/v1/uploads | Get presigned upload URL |
| POST   | /api/v1/uploads/:id/complete | Confirm upload, queue transcode |
| POST   | /webhooks/razorpay | Payment webhooks |

Full request/response docs in [docs/API.md](docs/API.md).

## Testing

- Unit tests use hand-written in-memory fakes, no mock frameworks
- 50-goroutine concurrent webhook test proving exactly-once payment capture
- Integration tests for dispatcher crash recovery and transcoder worker failover
- Race detector runs in a Docker container via `make test-race`

## Project layout

```
cmd/
  api/             HTTP server
  dispatcher/      Outbox → Kafka
  transcoder/      FFmpeg workers
  migrate/         SQL migrations
  seed/            Dev seeder
internal/
  campaign/        Campaign + tier domain
  pledge/          Pledge, payments, ledger
    gateway/       Payment gateway interface
      fake/        In-memory fake (auto-activates in dev)
      razorpay/    Real Razorpay client
  media/           Uploads + transcoding
    transcode/     FFmpeg pipeline
  outbox/          Event dispatcher
  platform/        Infrastructure (config, errors, logging, S3, postgres)
migrations/        Raw SQL (16 pairs, embedded via embed.FS)
deploy/            Docker Compose
scripts/           Demo and test helpers
web/               React frontend (Vite)
```

## License

MIT
