# 18 — Local Dev & Deploy

---

## 1. Prerequisites

| Tool | Version | Notes |
| --- | --- | --- |
| Go | 1.26+ | |
| Docker + Compose | v2 | 8 GB RAM allocated minimum — Kafka + Mongo + Postgres + MinIO is not light |
| FFmpeg / ffprobe | 6.x | only if running `cmd/transcoder` outside Docker |
| `golang-migrate` | latest | or `goose` |
| `mc` (MinIO client) | latest | optional; the compose init handles buckets |
| `ngrok` | latest | to receive real Razorpay test webhooks |

---

## 2. First run

```bash
git clone https://github.com/maczeo11/cinefund && cd cinefund
cp .env.example .env          # fill in the Razorpay test keys
make up                       # infra
make migrate                  # postgres schema + mongo indexes
make topics                   # kafka topics with correct partition counts
make seed                     # dev data
make run-api                  # :8080 HTTP, :9090 gRPC
```

In separate terminals:

```bash
make run-dispatcher
make run-mediawatcher
make run-transcoder
make run-scheduler
```

`make dev` runs all of them under a process manager if you'd rather have one
terminal. Keep them separable though — being able to kill just the dispatcher and
watch `outbox_lag_seconds` climb is a useful thing to be able to do.

---

## 3. `docker-compose.yml`

```yaml
name: cinefund

services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: cinefund
      POSTGRES_PASSWORD: cinefund
      POSTGRES_DB: cinefund
    ports: ["5432:5432"]
    volumes: ["pg_data:/var/lib/postgresql/data"]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U cinefund"]
      interval: 5s
      retries: 20

  mongo:
    image: mongo:7
    command: ["--replSet", "rs0", "--bind_ip_all"]
    ports: ["27017:27017"]
    volumes: ["mongo_data:/data/db"]
    healthcheck:
      test: >
        mongosh --quiet --eval "
        try { rs.status().ok }
        catch (e) { rs.initiate({_id:'rs0',members:[{_id:0,host:'mongo:27017'}]}).ok }"
      interval: 5s
      retries: 30

  redis:
    image: redis:7-alpine
    command: ["redis-server", "--maxmemory", "256mb", "--maxmemory-policy", "volatile-lru"]
    ports: ["6379:6379"]
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      retries: 20

  kafka:
    image: bitnami/kafka:3.7
    ports: ["9092:9092"]
    environment:
      KAFKA_CFG_NODE_ID: "0"
      KAFKA_CFG_PROCESS_ROLES: "controller,broker"
      KAFKA_CFG_CONTROLLER_QUORUM_VOTERS: "0@kafka:9093"
      KAFKA_CFG_LISTENERS: "PLAINTEXT://:9092,CONTROLLER://:9093"
      KAFKA_CFG_ADVERTISED_LISTENERS: "PLAINTEXT://localhost:9092"
      KAFKA_CFG_CONTROLLER_LISTENER_NAMES: "CONTROLLER"
      KAFKA_CFG_OFFSETS_TOPIC_REPLICATION_FACTOR: "1"
      KAFKA_CFG_TRANSACTION_STATE_LOG_REPLICATION_FACTOR: "1"
      KAFKA_CFG_TRANSACTION_STATE_LOG_MIN_ISR: "1"
      KAFKA_CFG_AUTO_CREATE_TOPICS_ENABLE: "false"
    volumes: ["kafka_data:/bitnami/kafka"]
    healthcheck:
      test: ["CMD-SHELL", "kafka-topics.sh --bootstrap-server localhost:9092 --list || exit 1"]
      interval: 10s
      retries: 20

  minio:
    image: minio/minio
    command: ["server", "/data", "--console-address", ":9001"]
    environment:
      MINIO_ROOT_USER: minioadmin
      MINIO_ROOT_PASSWORD: minioadmin
    ports: ["9000:9000", "9001:9001"]
    volumes: ["minio_data:/data"]
    healthcheck:
      test: ["CMD", "mc", "ready", "local"]
      interval: 5s
      retries: 20

  minio-init:
    image: minio/mc
    depends_on: { minio: { condition: service_healthy } }
    entrypoint: >
      /bin/sh -c "
      mc alias set local http://minio:9000 minioadmin minioadmin &&
      mc mb -p local/cinefund-originals local/cinefund-media
            local/cinefund-public local/cinefund-backup &&
      mc anonymous set download local/cinefund-public &&
      mc anonymous set none local/cinefund-media &&
      mc anonymous set none local/cinefund-originals"

  mailhog:
    image: mailhog/mailhog
    ports: ["1025:1025", "8025:8025"]     # SMTP, web UI

volumes:
  pg_data: {}
  mongo_data: {}
  kafka_data: {}
  minio_data: {}
```

### Notes on choices here

- **`KAFKA_CFG_ADVERTISED_LISTENERS: PLAINTEXT://localhost:9092`** because you'll
  run Go processes on the host during development. If you also run them in
  Docker, you need two listeners (internal `kafka:9092`, external
  `localhost:29092`) — the same class of problem as the MinIO endpoint split in
  [10 §6](10-OBJECT-STORAGE.md#6-minio-locally). Expect to hit it.
- **`volatile-lru`** on Redis so keys without a TTL (locks, rate-limit buckets)
  are never evicted while cache entries are.
- **`AUTO_CREATE_TOPICS_ENABLE: false`** so a typo'd topic name fails loudly
  instead of silently creating a topic nobody consumes.
- **MailHog** catches every outbound email at http://localhost:8025. Never wire a
  real SMTP provider into development.

---

## 4. `.env.example`

```bash
APP_ENV=development
PORT=8080
GRPC_PORT=9090
LOG_LEVEL=info

# Postgres
POSTGRES_URL=postgres://cinefund:cinefund@localhost:5432/cinefund?sslmode=disable
POSTGRES_MAX_CONNS=25
POSTGRES_MIN_CONNS=5

# Mongo — replicaSet + directConnection are REQUIRED for change streams locally
MONGO_URI=mongodb://localhost:27017/?replicaSet=rs0&directConnection=true
MONGO_DATABASE=cinefund

# Redis
REDIS_URL=redis://localhost:6379/0
REDIS_READ_TIMEOUT=50ms
CACHE_VERSION=v1
CACHE_ENABLED=true

# Kafka
KAFKA_BROKERS=localhost:9092
KAFKA_CLIENT_ID=cinefund

# Object storage — two endpoints, deliberately (see doc 10 §6)
S3_ENDPOINT=localhost:9000
S3_PUBLIC_ENDPOINT=localhost:9000
S3_ACCESS_KEY=minioadmin
S3_SECRET_KEY=minioadmin
S3_BUCKET_ORIGINALS=cinefund-originals
S3_BUCKET_MEDIA=cinefund-media
S3_BUCKET_PUBLIC=cinefund-public
S3_USE_SSL=false
S3_REGION=us-east-1

# Auth — must be >= 32 bytes and DIFFERENT from each other
JWT_ACCESS_SECRET=
JWT_REFRESH_SECRET=
ACCESS_TOKEN_TTL=15m
REFRESH_TOKEN_TTL=720h

# Razorpay (test mode)
RAZORPAY_KEY_ID=
RAZORPAY_KEY_SECRET=
RAZORPAY_WEBHOOK_SECRET=

# Platform
PLATFORM_FEE_PERCENT=7
EARLY_ACCESS_DAYS=30

# Transcoding
FFMPEG_PATH=ffmpeg
FFPROBE_PATH=ffprobe
TRANSCODE_CONCURRENCY=2
FFMPEG_THREADS=4
TRANSCODE_WORK_DIR=/var/tmp/cinefund
TRANSCODE_MIN_FREE_BYTES=10737418240
TRANSCODE_JOB_TIMEOUT=2h
TRANSCODE_LEASE_TTL=60s
PIPELINE_VERSION=1
HLS_SEGMENT_SECONDS=6

# Email
SMTP_HOST=localhost
SMTP_PORT=1025
EMAIL_FROM=noreply@cinefund.local

# Telemetry
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
TRACE_SAMPLE_RATIO=1.0
```

Generate secrets:

```bash
openssl rand -hex 32
```

CI asserts `.env` and `.env.example` have identical key sets. A missing
environment variable found in production is entirely avoidable.

---

## 5. Razorpay webhooks locally

```bash
ngrok http 8080
# → https://a1b2c3.ngrok-free.app
```

Set the webhook URL in the Razorpay dashboard to
`https://a1b2c3.ngrok-free.app/webhooks/razorpay`, subscribe to
`payment.authorized`, `payment.captured`, `payment.failed`, `refund.processed`,
`refund.failed`, and copy the webhook secret into `.env`.

For fast iteration (and for hammering scenario P2), replay signed payloads
locally:

```bash
# scripts/fake-webhook.sh
BODY=$(cat "${1:-testdata/razorpay/payment_captured.json}")
SIG=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$RAZORPAY_WEBHOOK_SECRET" -hex | awk '{print $2}')
curl -s -X POST http://localhost:8080/webhooks/razorpay \
     -H "Content-Type: application/json" \
     -H "X-Razorpay-Signature: $SIG" \
     --data-raw "$BODY" -w '\n%{http_code}\n'
```

`--data-raw` matters — `--data` mangles whitespace, which breaks the HMAC. That
is a genuinely annoying 30 minutes if you don't know it.

To test idempotency: `for i in $(seq 50); do ./scripts/fake-webhook.sh & done; wait`

---

## 6. Observability stack

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.obs.yml up -d
```

| Service | URL |
| --- | --- |
| Grafana | http://localhost:3001 (admin/admin) |
| Prometheus | http://localhost:9091 |
| Jaeger | http://localhost:16686 |
| MinIO console | http://localhost:9001 |
| MailHog | http://localhost:8025 |

Kept in a separate compose file so day-to-day development doesn't pay for four
extra containers.

---

## 7. Production shape

A single VM, Docker Compose, Caddy for TLS. Not Kubernetes — see
[01 §7](01-ARCHITECTURE.md#7-what-deliberately-isnt-here).

```
┌──────────────────────────────────────────┐
│ VM (4 vCPU, 16 GB, 200 GB SSD)           │
│                                          │
│  Caddy (TLS, :443) ──► api ×2            │
│                        dispatcher ×1     │
│                        mediawatcher ×1   │
│                        transcoder ×2     │
│                        notifier ×1       │
│                        scheduler ×1      │
│                                          │
│  postgres, mongo, redis, kafka           │
└──────────────────────────────────────────┘
        │
        └──► S3 (media) ──► CDN
```

Differences from local:

| Concern | Production |
| --- | --- |
| TLS | Caddy, automatic Let's Encrypt |
| Object storage | real S3 (or MinIO on a separate volume), CDN in front of renditions |
| Secrets | injected by the orchestrator, never in a committed file |
| Postgres | daily `pg_dump` to `cinefund-backup`, 30-day retention, **restore tested monthly** |
| Mongo | `mongodump` daily |
| Sampling | `TRACE_SAMPLE_RATIO=0.1`, money paths forced to 1.0 |
| Log level | `info` |
| `scheduler` | exactly 1 replica, plus the Redis lease as a safety net |
| `mediawatcher` | exactly 1 replica (single resume token) |
| `transcoder` | CPU-limited so FFmpeg can't starve Postgres |

**A backup you have never restored is not a backup.** Put a monthly restore drill
in the runbook — restore into a scratch database and run reconciliation checks
R1–R7 against it. If they pass, the backup is real.

---

## 8. Deploy

```bash
git pull
docker compose build api transcoder dispatcher mediawatcher notifier scheduler
docker compose run --rm api ./migrate up      # migrations FIRST, as a separate step
docker compose up -d --no-deps api dispatcher mediawatcher transcoder notifier scheduler
docker compose ps
curl -sf https://cinefund.example/health/ready
```

**Migrations run as their own step**, never on API startup. Two API replicas
racing to migrate on deploy is a genuinely bad afternoon.

Every migration must be **backwards-compatible with the currently-running code**,
because for a moment both versions are live. Adding a `NOT NULL` column without a
default breaks the old code instantly. The safe sequence is always:

1. Add the column nullable, deploy code that writes it.
2. Backfill.
3. Add the `NOT NULL` constraint in a later migration.

Three deploys instead of one, and no downtime.

---

## 9. Runbook

### Outbox lag is climbing

```bash
docker compose logs --tail 100 dispatcher
psql -c "SELECT count(*), min(created_at), max(attempts) FROM outbox WHERE published_at IS NULL;"
psql -c "SELECT event_type, last_error, count(*) FROM outbox
          WHERE published_at IS NULL AND last_error IS NOT NULL
          GROUP BY 1,2 ORDER BY 3 DESC LIMIT 10;"
```

Kafka unreachable → fix Kafka, the backlog drains itself. One poison event with a
high `attempts` count → inspect it, fix or manually mark it published with a
recorded reason.

### A pledge is stuck in CREATED

```bash
psql -c "SELECT id, provider_order_id, created_at FROM pledges WHERE id='…';"
psql -c "SELECT event_type, processed_at FROM payment_events WHERE pledge_id='…';"
```

No `payment_events` row → the webhook never arrived. Check the Razorpay dashboard
delivery log and the ngrok/Caddy access log. The reconciliation sweep resolves
these automatically within 15 minutes; if it hasn't, the sweep isn't running.

### Transcode jobs are queuing

```bash
mongosh --eval 'db.transcode_jobs.aggregate([{$group:{_id:"$status",n:{$sum:1}}}])'
docker compose logs --tail 200 transcoder | grep -i ffmpeg
df -h /var/tmp
```

Disk full is the most common cause. Second most common: `realtime_factor` below
1.0, meaning you need more transcoder replicas or fewer concurrent encodes per
replica.

### Reconciliation reported a failure

**This is the serious one.** Do not auto-correct.

```bash
curl -s localhost:8080/api/v1/admin/reconciliation/latest | jq
```

Identify the failing check (R1–R7), find the affected rows, read the `audit_log`
and `payment_events` for the entities involved, and reconstruct what happened
before writing any `ADJUSTMENT`. Every adjustment needs an admin id and a memo.

### Emergency: stop taking money

```bash
docker compose exec redis redis-cli SET cf:v1:killswitch:pledges 1
```

`POST /pledges` checks this key and returns 503 with a maintenance message.
Webhooks keep processing so in-flight payments still settle correctly — that
asymmetry is deliberate and important: stop *new* money, never stop *recording*
money.

---

## 10. Cost sketch

| Item | Monthly |
| --- | --- |
| VM (4 vCPU, 16 GB) | ~$40 |
| S3, 500 GB | ~$12 |
| CDN egress, 1 TB | ~$10–80 depending on provider |
| Domain + TLS | ~$1 (Caddy is free) |
| **Total** | **~$65–130** |

Transcoding is the CPU cost and egress is the bandwidth cost. Both scale with
content, not users — worth knowing when someone asks how this scales, because the
answer is "the expensive part is video, and the expensive part of video is
egress, so the first optimisation is a CDN, not more servers."
