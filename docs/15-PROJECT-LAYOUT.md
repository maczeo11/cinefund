# 15 — Project Layout

The exact file tree, and what every package owns. Read this before creating a
file — "where does this go" should never be a decision you make twice.

---

## 1. The tree

```
cinefund/
├── go.mod                       module github.com/maczeo11/cinefund
├── go.sum
├── Makefile
├── README.md
├── .env.example
├── .golangci.yml
│
├── cmd/
│   ├── api/main.go              HTTP + gRPC server
│   ├── dispatcher/main.go       Postgres outbox → Kafka
│   ├── mediawatcher/main.go     Mongo change stream → Kafka
│   ├── transcoder/main.go       Kafka → FFmpeg worker pool
│   ├── notifier/main.go         Kafka → email
│   ├── scheduler/main.go        cron: deadlines, reconciliation, rollups
│   ├── migrate/main.go          schema migrations + Kafka topic creation
│   └── seed/main.go             idempotent dev data
│
├── internal/
│   ├── platform/                infrastructure. Knows nothing about the domain.
│   │   ├── config/              env → typed Config, validated at boot
│   │   ├── logger/              slog setup, redaction, ctx helpers
│   │   ├── telemetry/           otel tracer, prometheus registry, propagation
│   │   ├── errs/                typed errors + HTTP status mapping
│   │   ├── httpx/               middleware, response envelopes, cursor pagination
│   │   ├── authz/               Actor, CanActOn, role constants
│   │   ├── crypto/              argon2 passwords, HMAC verification
│   │   ├── postgres/            pool, tx runner, pgerr helpers
│   │   ├── mongodb/             client, change-stream helper, resume tokens
│   │   ├── redisx/              client, cache, locker, rate limiter (+ Lua)
│   │   ├── kafkax/              producer, consumer loop, envelope, retry/DLQ
│   │   ├── objectstore/         Store interface, s3Store, memStore
│   │   └── validate/            validator setup, custom rules
│   │
│   ├── identity/                users, sessions, creator profiles
│   ├── campaign/                campaigns, tiers, state machine
│   ├── pledge/                  pledges, payments, ledger, refunds, payouts
│   │   ├── gateway/razorpay/    the only package that imports Razorpay
│   │   └── ledger.go
│   ├── media/                   assets, uploads, transcode jobs
│   │   └── transcode/           ffmpeg, ffprobe, ladder, HLS, worker pool
│   ├── catalog/                 Mongo read models + projector
│   ├── playback/                entitlements, signed URLs, playlist rewriter
│   ├── notification/            email templates + consumers
│   ├── outbox/                  writer (used by services) + dispatcher
│   ├── scheduler/               jobs: deadline sweep, reconcile, rollups
│   └── gen/transcodev1/         generated protobuf — committed
│
├── proto/cinefund/transcode/v1/transcode.proto
├── migrations/                  0001_*.up.sql / .down.sql
├── api/openapi.yaml
├── deploy/
│   ├── docker-compose.yml
│   ├── docker-compose.obs.yml   prometheus + grafana + jaeger
│   ├── Dockerfile.api
│   ├── Dockerfile.transcoder    the one with ffmpeg
│   ├── grafana/dashboards/
│   └── prometheus/prometheus.yml
├── scripts/                     dev helpers: fake-webhook.sh, make-sample-video.sh
├── testdata/                    tiny sample media, golden JSON
└── docs/                        this folder
```

---

## 2. Why `internal/` for everything

`internal/` is enforced by the Go toolchain: nothing outside this module can
import it. That's the whole reason. It means you can restructure freely without
worrying about who depends on what, and there is no accidental public API.

Nothing lives in `pkg/`. If a package genuinely becomes reusable, promote it
then — not in anticipation.

---

## 3. Package responsibilities

### `platform/*` — infrastructure only

The rule: **`platform` never imports a domain package.** If `platform/httpx`
needs to know about a campaign, the abstraction is wrong. Dependencies point
domain → platform, never the reverse. This is the one architectural rule worth
enforcing with a lint (`depguard` in `.golangci.yml`).

| Package | Owns | Key types |
| --- | --- | --- |
| `config` | env parsing, validation, defaults | `Config`, `MustLoad()` |
| `logger` | slog handler, redaction, `FromContext` | |
| `telemetry` | tracer provider, metric registry, Kafka header propagation | |
| `errs` | `Kind`, constructors, `HTTPStatus(err)` | `errs.NotFound(...)` |
| `httpx` | middleware chain, `Respond`, `Cursor` | |
| `authz` | `Actor`, `CanActOn`, `Role` | |
| `crypto` | `HashPassword`, `VerifyPassword`, `VerifyHMAC` | |
| `postgres` | `Pool`, `TxRunner`, `IsUnique(err)` | `tx.Do(ctx, fn)` |
| `mongodb` | client, `Watch` helper, resume-token store | |
| `redisx` | `Cache`, `Locker`, `Limiter` + embedded Lua | |
| `kafkax` | `Producer`, `ConsumerGroup`, `Envelope`, DLQ routing | |
| `objectstore` | `Store` interface, S3 + in-memory impls | |
| `validate` | validator instance, `Struct(v)` → `errs.Invalid` | |

### Domain packages — the four-file shape

Every one of `identity`, `campaign`, `pledge`, `media`, `catalog`, `playback`
follows the same layout, described in [01 §2](01-ARCHITECTURE.md#2-layering-inside-a-domain-package):

```
internal/campaign/
├── model.go        types, state machine, invariants. Zero infra imports.
├── repo.go         SQL. Takes/returns model types.
├── repo_test.go    integration, testcontainers
├── service.go      business logic, transaction boundaries
├── service_test.go unit, with a fake repo
├── handler.go      HTTP binding + response shaping
├── handler_test.go httptest with a fake service
└── routes.go       route registration for this domain
```

`routes.go` per domain, rather than one central router file, means adding an
endpoint touches one package. `cmd/api/main.go` just calls
`campaign.RegisterRoutes(r, svc)` for each.

### `pledge/` is the big one

It owns pledges, payments, the ledger, refunds and payouts because they are one
transactional unit — you cannot capture a pledge without writing ledger entries,
so splitting them across packages would mean exporting a transaction handle
between packages. Keep them together.

```
internal/pledge/
├── model.go            Pledge, state machine, Refund, Payout
├── repo.go
├── service.go          CreatePledge, HandleWebhook, InitiateRefund
├── ledger.go           accounts, entries, RecordPledgeCapture/Refund/Payout
├── ledger_test.go      the balance tests from doc 07
├── webhook.go          signature verify + event dispatch
├── reconcile.go        called by the scheduler
├── handler.go
├── routes.go
└── gateway/
    ├── gateway.go      interface: CreateOrder, FetchPayments, CreateRefund
    ├── razorpay/       the ONLY package importing the Razorpay SDK
    └── fake/           deterministic test double
```

The `gateway.Gateway` interface is what makes every payment test possible without
a network. It's a three-method interface — the smallest thing that could work,
which is exactly what it should be.

---

## 4. Interfaces are declared by the consumer

This is the single most important Go convention in the codebase, and the thing
the MagicStream roadmap identified as its biggest gap.

```go
// internal/campaign/service.go — the CONSUMER declares what it needs
type Repository interface {
    GetCampaign(ctx context.Context, id uuid.UUID) (*Campaign, error)
    UpdateCampaign(ctx context.Context, c *Campaign) error
}

type Service struct {
    repo Repository        // interface
    tx   postgres.TxRunner
    log  *slog.Logger
}
```

`repo.go` provides `*PostgresRepository`, which satisfies it implicitly — with no
`implements` declaration and no import from repo to service. Three consequences:

1. `service_test.go` uses a hand-written fake. No database, milliseconds.
2. The interface stays minimal, because it only lists what this consumer uses.
3. Swapping the implementation touches one line in `main.go`.

Do **not** declare a giant `Repository` interface in `repo.go` listing all 30
methods. That's the pattern that produces interfaces nobody can fake.

---

## 5. Wiring in `main.go`

Explicit constructor calls, top to bottom. No DI framework, no reflection, no
container.

```go
func main() {
    cfg := config.MustLoad()
    log := logger.New(cfg.Log)
    shutdownTel := telemetry.MustInit(cfg.Telemetry)
    defer shutdownTel(context.Background())

    pg    := postgres.MustConnect(ctx, cfg.Postgres)
    mg    := mongodb.MustConnect(ctx, cfg.Mongo)
    rdb   := redisx.MustConnect(cfg.Redis)
    store := objectstore.MustNew(cfg.S3)
    kprod := kafkax.MustProducer(cfg.Kafka)

    cache   := redisx.NewCache(rdb, cfg.CacheVersion)
    limiter := redisx.NewLimiter(rdb)
    tx      := postgres.NewTxRunner(pg)

    gw          := razorpay.New(cfg.Razorpay)
    outboxW     := outbox.NewWriter()
    identitySvc := identity.NewService(identity.NewRepo(pg), tx, log)
    campaignSvc := campaign.NewService(campaign.NewRepo(pg), catalog.NewRepo(mg), cache, tx, outboxW, log)
    pledgeSvc   := pledge.NewService(pledge.NewRepo(pg), gw, rdb, tx, outboxW, log)
    mediaSvc    := media.NewService(media.NewRepo(mg), store, log)
    playbackSvc := playback.NewService(playback.NewRepo(pg), catalog.NewRepo(mg), store, cache, log)

    r := httpx.NewRouter(cfg, log, limiter)
    identity.RegisterRoutes(r, identitySvc)
    campaign.RegisterRoutes(r, campaignSvc)
    pledge.RegisterRoutes(r, pledgeSvc)
    media.RegisterRoutes(r, mediaSvc)
    playback.RegisterRoutes(r, playbackSvc)

    // ... http.Server + grpc.Server, errgroup, graceful shutdown
}
```

You can read the entire dependency graph in one screen. That is worth more than
any amount of framework magic, and when something is nil at 2 a.m. you know
exactly where it was supposed to be constructed.

### Graceful shutdown

```go
g, ctx := errgroup.WithContext(ctx)
g.Go(func() error { return httpSrv.ListenAndServe() })
g.Go(func() error { return grpcSrv.Serve(lis) })
g.Go(func() error {
    <-ctx.Done()
    sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()
    grpcSrv.GracefulStop()
    return httpSrv.Shutdown(sctx)
})
```

Consumers get the same treatment: on `SIGTERM`, stop polling, finish in-flight
messages, commit offsets, close. A consumer killed mid-message reprocesses on
restart — safe, because consumers are idempotent, but a clean shutdown avoids
the duplicate entirely.

---

## 6. Configuration

One struct, loaded once, validated at boot, passed down. No `os.Getenv` below
`config`.

```go
type Config struct {
    Env       string  `env:"APP_ENV" envDefault:"development"`
    Port      int     `env:"PORT"    envDefault:"8080"`
    GRPCPort  int     `env:"GRPC_PORT" envDefault:"9090"`
    Postgres  PostgresConfig
    Mongo     MongoConfig
    Redis     RedisConfig
    Kafka     KafkaConfig
    S3        S3Config
    Razorpay  RazorpayConfig
    JWT       JWTConfig
    Transcode TranscodeConfig
}

func MustLoad() Config {
    var c Config
    if err := env.Parse(&c); err != nil { log.Fatalf("config: %v", err) }
    if err := c.Validate(); err != nil  { log.Fatalf("config invalid: %v", err) }
    return c
}
```

`Validate()` enforces the rules that would otherwise fail at 3 a.m.: JWT secrets
present, ≥ 32 bytes and distinct; Razorpay webhook secret present when
`APP_ENV=production`; `TRANSCODE_CONCURRENCY ≥ 1`. **Fail at boot, loudly.**

---

## 7. Makefile

```makefile
.PHONY: help up down migrate seed run test test-int lint proto topics

up:        ## start all infrastructure
	docker compose -f deploy/docker-compose.yml up -d

migrate:   ## apply schema migrations
	go run ./cmd/migrate up

topics:    ## create kafka topics with correct partitions
	go run ./cmd/migrate topics

seed:      ## idempotent dev data
	go run ./cmd/seed

run-api:        ; go run ./cmd/api
run-dispatcher: ; go run ./cmd/dispatcher
run-transcoder: ; go run ./cmd/transcoder

test:      ## unit tests only, no docker
	go test ./... -short -race

test-int:  ## integration tests, needs docker
	go test ./... -race -tags=integration

lint:      ; golangci-lint run
proto:     ; buf generate
proto-check: proto ; git diff --exit-code -- internal/gen
```

`-short` on unit tests and a build tag on integration tests means `make test`
stays under 10 seconds and you will actually run it. An integration-only test
suite that takes four minutes is a suite that gets run once a day.

---

## 8. Lint config

`.golangci.yml` — the linters that catch real bugs, not style opinions:

```yaml
linters:
  enable:
    - errcheck        # unchecked errors
    - govet
    - staticcheck
    - ineffassign
    - bodyclose       # unclosed HTTP response bodies
    - rowserrcheck    # unchecked sql.Rows.Err()
    - sqlclosecheck
    - noctx           # HTTP requests without a context
    - contextcheck
    - depguard        # enforce the platform→domain direction
    - gosec
    - goleak

linters-settings:
  depguard:
    rules:
      platform-is-pure:
        files: ["**/internal/platform/**"]
        deny:
          - pkg: "github.com/maczeo11/cinefund/internal/campaign"
          - pkg: "github.com/maczeo11/cinefund/internal/pledge"
          - pkg: "github.com/maczeo11/cinefund/internal/media"
          - pkg: "github.com/maczeo11/cinefund/internal/identity"
```

`depguard` is what makes the layering rule from §3 real rather than aspirational.
`bodyclose` and `noctx` each catch a class of bug that is invisible in review and
obvious in production.

---

## 9. Where things go — the lookup table

When you don't know where a file belongs:

| I'm writing… | It goes in |
| --- | --- |
| a new HTTP endpoint | `internal/<domain>/handler.go` + `routes.go` |
| a business rule | `internal/<domain>/service.go` |
| a state transition rule | `internal/<domain>/model.go` |
| a SQL query | `internal/<domain>/repo.go` |
| a Mongo query for a read model | `internal/catalog/repo.go` |
| middleware | `internal/platform/httpx/` |
| a Kafka consumer | `internal/<domain>/consumer.go` |
| an event payload type | `internal/<domain>/events.go` |
| an FFmpeg concern | `internal/media/transcode/` |
| a Razorpay call | `internal/pledge/gateway/razorpay/` |
| a cron job | `internal/scheduler/jobs/` |
| a shared helper with no domain meaning | `internal/platform/…` |
| a shared helper *with* domain meaning | the domain package that owns the concept |

The last two rows are the ones people get wrong. `internal/platform/util` is
where code goes to die — if a helper knows what a campaign is, it belongs in
`campaign`.
