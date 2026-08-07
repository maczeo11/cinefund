# AGENTS.md

Guidance for AI agents working in this repository. Read this before editing.

## What this project is

**CineFund** — crowdfunding + streaming for short films. Creators launch
campaigns, backers pledge money, funded films get transcoded and streamed.
Go 1.26.3, module `github.com/maczeo11/cinefund`.

The interesting parts (and the resume claims): webhook idempotency, a
transactional outbox, a double-entry ledger, an FFmpeg ABR pipeline. Four
"quietly wrong" problems are documented in `README.md` and `docs/00`.

## Architecture

**Modular monolith + worker binaries** — one Go module, several binaries in
`cmd/`, all sharing `internal/`. Postgres is the **only** datastore (Mongo was
dropped before any code — ADR-0010). Kafka is the event bus, fed by a Postgres
outbox.

```
cmd/api         HTTP server (Gin) — currently health endpoints only
cmd/migrate     migration runner: go run ./cmd/migrate up|down|status
cmd/transcoder  FFmpeg worker: claims jobs from Postgres, runs FFmpeg, uploads
cmd/dispatcher  OUTBOX→Kafka dispatcher — EMPTY DIR, NOT BUILT
cmd/seed        dev data — EMPTY DIR, NOT BUILT

internal/
  platform/       config, logger, errs, crypto, postgres, objectstore
  pledge/         pledges, payments, ledger, refunds, payouts (the money spine)
    gateway/        gateway.go (interface) + fake/ (test double) + razorpay/ (EMPTY)
  media/          media assets + transcode jobs (repo.go + transcode/ subpackage)
  campaign/       EMPTY DIR, NOT BUILT
  identity/       EMPTY DIR, NOT BUILT
  outbox/         EMPTY DIR, NOT BUILT
```

## Layering rules (enforced by convention)

- `handler` parses/validates → `service` decides → `repo` persists. A handler
  touching pgx directly is a bug.
- **The transaction boundary lives in the service.** Services depend on small
  interfaces (`Repository`, `TxRunner`, `Queries`, `Gateway`, `Redis`) instead
  of concrete pools, so "what's inside the transaction" is explicit and testable.
  A repo method that opens its own transaction inside a service call is a bug.
- `model.go` has no imports of pgx/gin/SDK — if you can't unit-test the state
  machine with zero infrastructure, the layering has leaked.
- No `os.Getenv` below `internal/platform/config`. Values are parsed once at
  boot into a typed `Config`.
- Errors are typed via `internal/platform/errs` (`Kind` + `Code` + `Message`).
  Handlers never call `c.JSON(500,...)`; the error middleware maps `Kind`→HTTP
  status in exactly one place (`errs.HTTPStatus`).
- Money is `BIGINT` **paise**, never float. `golangci-lint` isn't installed on
  this machine; run `go build ./...` and `go vet ./...` instead.
- No comments in code unless they carry real information. This repo DOES use
  dense explanatory comments on non-obvious decisions (webhook idempotency,
  GOP alignment, fencing checks) — match that style where a decision isn't
  obvious from the code, don't add "// increments counter" noise.

## Commands

```bash
cp .env.example .env            # required: config.MustLoad reads it via godotenv
make up                         # postgres, redis, kafka, minio in Docker
make migrate                    # go run ./cmd/migrate up
go run ./cmd/api                # :8080
go test ./... -short            # unit tests (offline, no Docker needed)
make test                       # NOTE: adds -race — see Windows caveat below
make test-int                   # go test -race -tags=integration (needs Docker)
```

**Windows caveat:** `go test -race` FAILS on this laptop (broken cc1 64-bit
mode). Run race detector tests in the Docker container (commit 3d807c8), or run
plain `go test ./...` locally. `golangci-lint` is not installed.

**Local infra facts:**
- Postgres maps host **5433**→container 5432 (a local Postgres service owns
  host 5432). DSN: `postgres://cinefund:cinefund@localhost:5433/cinefund?sslmode=disable`
- Kafka: `bitnamilegacy/kafka:3.9.0` (KRaft, no ZooKeeper). Tag was renamed
  upstream from `bitnami/kafka` — do not revert it.
- MinIO on 9000/9001, two private buckets (`cinefund-originals`, `cinefund-media`).
- Fake payment gateway is used automatically when Razorpay keys are empty
  outside production (`config.UseFakeGateway()`).

## Testing strategy

Unit tests are fully offline using test doubles:
- `internal/pledge/gateway/fake` — deterministic gateway; `Capture()` emits a
  correctly HMAC-signed webhook body (exactly how Razorpay signs).
- `fake_redis_test.go` — in-memory Redis with `Flush()` to simulate Redis losing
  a key (test P5).
- `fake_repo_test.go` — in-memory `Queries` that emulates the `uq_provider_event`
  unique constraint (the real idempotency guarantee).

Test IDs in comments map to the plan in `docs/16`: P1/P2/P5/P6 (pledges),
M2/M3/M4/M7/M8/M9 (media), L5 (ledger), R5 (rate limit, not built). Keep the
convention: name the test and cite the ID in a comment.

39 test functions currently, all passing with `go test ./... -short`.

Integration tests (Postgres+Redis+Kafka, `-tags=integration`) are declared in
the plan but not yet written.

## Key domain logic — webhook idempotency (the crown jewel)

`internal/pledge/service.go` `HandleWebhook` — two-layer guard:

1. **Layer 1 (fast path):** Redis `SETNX idem:wh:{event_id}` 24h. Absorbs a
   retry storm without touching Postgres. NOT a guarantee — Redis can lose it.
2. **Layer 2 (durable):** `payment_events` has `UNIQUE (provider, provider_event_id)`
   (`uq_provider_event`, migration 0008). The tx inserts the event FIRST; a
   23505 means "already processed" → `ErrDuplicateEvent` → HTTP 200 so the
   provider stops retrying.
3. **The subtle line:** on any non-duplicate failure, `DEL` the Redis key — else
   a provider retry is swallowed by our own fast path and the event is lost.

`applyCapture` order: get pledge by order_id (fallback to `notes.pledge_id` for
the P12 lost-attach recovery) → state transition check (`CanTransitionTo`) →
amount match (mismatch = error, no state change, test P6) → mark captured →
increment `campaigns.raised_amount` → increment tier `claimed_count` (guarded by
`chk_tier_not_oversold`) → ledger `RecordPledgeCapture` (fee+tax) → outbox row
(`pledge.captured`) — all in ONE transaction.

`VerifySignature` = `HMAC-SHA256(rawBody, secret)` constant-time compare
(`internal/platform/crypto`). **Must use the exact raw bytes** — re-marshalling
breaks HMAC on key order/whitespace.

## Key domain logic — pledge state machine

`internal/pledge/model.go`. Status values ARE the DB strings. Legal transitions
in the `allowed` map. **`CREATED → CAPTURED` is legal** because Razorpay
auto-capture collapses AUTHORIZED→CAPTURED into one `payment.captured` event
(docs/00 §5.3); AUTHORIZED is kept for a future manual-capture path. Terminal:
FAILED, REFUNDED, SETTLED, REFUND_FAILED.

## Key domain logic — ledger

`internal/pledge/ledger.go` + migration 0011. Double-entry. Idempotent via
`uq_ledger_txn_reference` (`IsUnique → nil`). Balance enforced by a DEFERRABLE
`AFTER INSERT` trigger (`assert_ledger_balanced`) that fails the COMMIT if a
transaction's entries don't sum to zero. Balances are never stored — computed by
the `ledger_balances` view. **Sign convention is debit-normal**: CAMPAIGN_ESCROW
shows NEGATIVE while holding money; present as `-balance` in UI.

## Key domain logic — FFmpeg pipeline

`internal/media/transcode/` + `internal/media/repo.go`:

- **Job claim** (`repo.go:30`): one statement `WITH claimed AS (UPDATE ... FOR
  UPDATE SKIP LOCKED ...) JOIN media_assets`. SKIP LOCKED lets N workers each
  grab a different row with zero coordination. Prefers QUEUED over expired
  leases.
- **Fencing / lease steal** (`worker.go:265`): heartbeat does
  `UPDATE ... WHERE id=$1 AND worker_id=$2 AND status='RUNNING'`. Zero rows =
  another worker reclaimed the job → cancel the job context → FFmpeg subprocess
  dies → don't write rendition keys. This is the one corrupting failure mode.
- **runJob pipeline**: presign source URL (FFmpeg reads over HTTP, never
  downloads 4 GB) → ffprobe → `Validate` (reject before spending CPU) →
  `LadderFor` (never upscales) → encode rungs in parallel (bounded by semaphore
  channel) → upload renditions → **write master playlist LAST** (the commit
  point) → `Succeed` (job SUCCEEDED + asset READY + outbox row, one tx).
- **GOP alignment** (`args.go`): `-r 24 -g 48 -keyint_min 48 -sc_threshold 0`
  identical on every rung — this is what makes ABR switching work. `-vf
  scale=-2:...:format=yuv420p` (10-bit ProRes → plays in Chrome). `-ss` before
  `-i` in PosterArgs (seek by keyframe, not decode-from-0). `init()` panics at
  build time if SegmentSeconds isn't a multiple of the GOP.
- **Master playlist**: `avc1.4d4028` codec string is DERIVED from encoder
  settings (`ladder.go:99`), not hardcoded, so it can't drift. Highest bandwidth
  first. `RewriteVariantPlaylist` turns relative segment paths into presigned
  URLs (private bucket) and rejects lines with `/`, `\`, `..` (test M12).
- `JobStore` interface is defined in the transcode package; `media.JobRepo`
  implements it. Enqueue is idempotent via `uq_job_asset_version`.

## Migrations

Custom runner (`cmd/migrate`), files embedded via `migrations/embed.go`
(`//go:embed`). Files: `0001`–`0016`, each with `.up.sql` + `.down.sql`.
Forward-only; never edit a committed migration. Runner applies each migration in
its own transaction and records in `schema_migrations`. Never run on API
startup — always a separate step (`make migrate`).

Schema highlights:
- 0001: pgcrypto + citext + `set_updated_at()` trigger (every mutable table has
  a BEFORE UPDATE trigger calling it).
- 0008: `payment_events` — `uq_provider_event` is the idempotency guarantee.
- 0011: ledger tables + deferred balance trigger + `ledger_balances` view.
- 0013: `outbox` — BIGINT identity PK (dispatcher reads in insert order),
  `event_id UUID UNIQUE` travels to Kafka, partial index `WHERE published_at IS NULL`.
- 0016: `media_assets` + `transcode_jobs`; `uq_job_asset_version` makes
  duplicate Kafka dispatch safe (M8).

## Configuration (`.env`)

`internal/platform/config` parses via `caarlos0/env` + `godotenv.Load()`.
Required vars: `POSTGRES_DSN`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`,
`JWT_ACCESS_SECRET`, `JWT_REFRESH_SECRET` (both ≥32 bytes and DIFFERENT).
`RAZORPAY_*` blank = fake gateway (dev). Validation failures `log.Fatalf` at boot
on purpose. `IsProduction()` flips Gin to release mode and requires real Razorpay.

## Git conventions

- Conventional commits: `feat(scope):`, `fix(infra):`, `test(transcode):`,
  `docs:`, `chore(test):`. The repo commits ADRs and design BEFORE code.
- No commit unless the user asks.
- Working tree should stay green: `go build ./...`, `go vet ./...`,
  `go test ./... -short`.

## Current state (what's built vs not)

**Built + tested (offline):** migration runner + 16 migrations; config/logger/
errs/postgres/objectstore platforms; pledge service (CreatePledge, HandleWebhook,
applyCapture/applyFailure, ledger, state machine) with full fake-gateway test
suite; media/transcode library (probe, ladder, args, master, progress, worker)
with pure-function tests.

**NOT built (next up):**
- HTTP routes entirely — `cmd/api/main.go:79` has `TODO(A2+): RegisterRoutes`.
  No handler exists for `POST /webhooks/razorpay` or `POST /campaigns/{id}/pledges`.
- `internal/campaign`, `internal/identity`, `internal/outbox`, `cmd/dispatcher`,
  `cmd/seed`, `internal/pledge/gateway/razorpay` are empty directories.
- No Kafka at runtime (no dispatcher), no auth, no rate limiting.
- No `testdata/sample_*.mp4`, no end-to-end FFmpeg run, no worker/runner unit
  tests. `make sample` generates a fixture with ffmpeg.
- Real Razorpay adapter, refunds, payouts, entitlements, gRPC: not started.

See `docs/16-BUILD-ORDER.md` for the phase plan (A0/A1 done, A2–A5 partial,
B3/B4 core done, everything else open).

## Docs

Design-first repo: `docs/00`–`19` plus `docs/DECISIONS/ADR-*`. `docs/03`
(Mongo data model) is OBSOLETE (banner on top). Superseded ADRs stay in the tree
as history. `docs/DEVLOG.md` is the running log of dead ends — keep it honest,
never fabricate entries. Reading order is in `README.md`.
