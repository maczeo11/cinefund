# 17 — Testing Strategy

What to test, at which level, and — just as important — what not to test.

---

## 1. The shape

Not a pyramid. A **diamond**: few pure-unit tests, many integration tests, few
end-to-end tests.

```
        ▲  e2e (5%)          full stack, docker compose, the demo path
       ███ integration (55%) real Postgres/Mongo/Redis/Kafka via testcontainers
      █████ unit (40%)       state machines, ladders, cursors, ledger arithmetic
```

The reason integration dominates here: **almost every bug in this system lives at
a boundary.** Transaction semantics, unique-constraint behaviour under
concurrency, `SKIP LOCKED` correctness, change-stream resumption, Lua atomicity.
None of those can be tested with a mock — a mock asserts what you *believe*
Postgres does. The whole point of phase 7 and 9 is that your beliefs about
concurrency are the thing under test.

Mocks are for things you *own the contract of* (a repository interface) and for
things you cannot run (Razorpay).

---

## 2. Unit tests

Fast, no Docker, run on every save. Guarded by `-short`.

**Test these:**

| Target | Why it's a good unit test |
| --- | --- |
| `campaign.CanTransitionTo` | pure function, ~40 cases, table-driven |
| the field-editability matrix | pure, and a mistake here is a security bug |
| `LadderFor(sourceHeight)` | pure, and "never upscale" is easy to regress |
| cursor encode/decode | pure, round-trip property |
| fee arithmetic (`net = gross − fee`) | pure, and rounding is where money errors live |
| FFmpeg progress parsing | pure over a fixture string |
| `sanitiseFilename` | pure, security-relevant |
| `codecString(profile, level)` | pure, and getting it wrong breaks iOS silently |
| entitlement resolution order | pure given inputs, and it's an authorisation decision |

```go
func TestCampaignTransitions(t *testing.T) {
    tests := []struct{ from, to Status; want bool }{
        {StatusDraft,    StatusInReview, true},
        {StatusInReview, StatusLive,     true},
        {StatusLive,     StatusFunded,   true},
        {StatusFailed,   StatusLive,     false},   // terminal
        {StatusDraft,    StatusLive,     false},   // must pass review
        {StatusReleased, StatusDraft,    false},
    }
    for _, tt := range tests {
        t.Run(fmt.Sprintf("%s→%s", tt.from, tt.to), func(t *testing.T) {
            if got := tt.from.CanTransitionTo(tt.to); got != tt.want {
                t.Errorf("got %v want %v", got, tt.want)
            }
        })
    }
}
```

**Service tests with a fake repo** are also unit tests. The consumer-declared
interface from [15 §4](15-PROJECT-LAYOUT.md#4-interfaces-are-declared-by-the-consumer)
is what makes them possible, and they're where you test orchestration logic
without paying for a database.

**Do not unit-test:** repository methods (test them against real Postgres),
handlers with a mocked service *and* a mocked router (you end up asserting Gin's
behaviour), or anything whose assertion is "the mock was called".

---

## 3. Integration tests

`testcontainers-go`. Real databases, real Redis, real Kafka. Build tag
`integration` so `make test` stays fast.

```go
//go:build integration

func TestMain(m *testing.M) {
    ctx := context.Background()
    pg  := startPostgres(ctx)     // runs migrations
    mg  := startMongo(ctx)        // initiates rs0
    rdb := startRedis(ctx)
    kfk := startKafka(ctx)        // creates topics
    code := m.Run()
    teardown(ctx, pg, mg, rdb, kfk)
    os.Exit(code)
}
```

### One container set per package, not per test

Starting Postgres takes ~2 s and Kafka ~8 s. Per test that's unusable. Start once
in `TestMain`; **isolate tests by truncating tables** between them:

```go
func resetDB(t *testing.T, pool *pgxpool.Pool) {
    t.Helper()
    _, err := pool.Exec(context.Background(), `
        TRUNCATE users, campaigns, reward_tiers, pledges, payment_events,
                 refunds, payouts, ledger_accounts, ledger_transactions,
                 ledger_entries, entitlements, outbox, idempotency_keys, audit_log
        RESTART IDENTITY CASCADE`)
    require.NoError(t, err)
}
```

`TRUNCATE ... CASCADE` is far faster than dropping and recreating the schema, and
`RESTART IDENTITY` means test assertions on sequential ids stay stable.

An alternative worth knowing: run each test in a transaction that always rolls
back. Faster still, but it **cannot test anything involving concurrency,
`SKIP LOCKED`, or deferred constraints** — which is most of what matters here.
Use truncation.

### The scenarios

Every numbered scenario in the other documents is an integration test:

| Suite | Source | Count |
| --- | --- | --- |
| Payments | [06 §8](06-PAYMENTS-RAZORPAY.md#8-test-scenarios) P1–P14 | 14 |
| Ledger | [07 §7](07-LEDGER.md#7-tests) L1–L7 | 7 |
| Eventing | [08 §10](08-EVENTING-OUTBOX-KAFKA.md#10-tests) E1–E10 | 10 |
| Media | [09 §12](09-MEDIA-PIPELINE.md#12-tests) M1–M13 | 13 |
| Storage | [10 §8](10-OBJECT-STORAGE.md#8-tests) S1–S9 | 9 |
| Caching | [11 §10](11-CACHING-REDIS.md#10-tests) C1–C9 | 9 |
| Rate limiting | [12 §9](12-RATE-LIMITING.md#9-tests) R1–R10 | 10 |
| gRPC | [13 §8](13-GRPC-CONTROL-PLANE.md#8-tests) G1–G9 | 9 |
| **Total** | | **81** |

That list is the test plan. You don't need to invent one.

---

## 4. Testing concurrency

The tests that justify the whole design, and the ones people skip because they're
awkward. They're not — they're ~20 lines each.

```go
func TestWebhookIdempotency_50Concurrent(t *testing.T) {
    ctx := context.Background()
    resetDB(t, pool)
    pledge := seedPledgeAwaitingCapture(t, 100_000)
    payload, sig := signedCaptureWebhook(t, pledge, "evt_abc123")

    var wg sync.WaitGroup
    codes := make([]int, 50)
    start := make(chan struct{})            // release all goroutines at once

    for i := range codes {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            <-start
            codes[i] = postWebhook(t, payload, sig)
        }(i)
    }
    close(start)
    wg.Wait()

    for _, c := range codes { require.Equal(t, 200, c) }

    var raised int64
    require.NoError(t, pool.QueryRow(ctx,
        `SELECT raised_amount FROM campaigns WHERE id=$1`, pledge.CampaignID).Scan(&raised))
    require.EqualValues(t, 100_000, raised)          // exactly once

    var events, txns int
    pool.QueryRow(ctx, `SELECT count(*) FROM payment_events`).Scan(&events)
    pool.QueryRow(ctx, `SELECT count(*) FROM ledger_transactions`).Scan(&txns)
    require.Equal(t, 1, events)
    require.Equal(t, 1, txns)
}
```

The `start` channel matters. Without it the goroutines launch staggered and the
first one finishes before the last one starts, so you've tested nothing. Release
them simultaneously.

Same pattern for: two dispatchers over 1000 outbox rows (E4), two pledges for the
last tier slot (P7), 100 requests against a limit of 10 (R5), 100 concurrent
cache misses (C4).

**Always run these with `-race`.** A test that passes without it and fails with
it has found a real bug.

---

## 5. Faking Razorpay

Never hit a real API in tests. The gateway interface has three methods:

```go
type Gateway interface {
    CreateOrder(ctx context.Context, req OrderRequest) (*Order, error)
    FetchPayments(ctx context.Context, orderID string) ([]Payment, error)
    CreateRefund(ctx context.Context, req RefundRequest) (*Refund, error)
}
```

```go
type Fake struct {
    mu       sync.Mutex
    orders   map[string]*Order
    failNext error                 // inject failures deterministically
    latency  time.Duration
}

func (f *Fake) CreateOrder(ctx context.Context, req OrderRequest) (*Order, error) {
    if f.failNext != nil { err := f.failNext; f.failNext = nil; return nil, err }
    o := &Order{ID: "order_test_" + req.Receipt, Amount: req.Amount, Receipt: req.Receipt}
    f.mu.Lock(); f.orders[o.ID] = o; f.mu.Unlock()
    return o, nil
}

// Produces a correctly-signed webhook payload for the test's HTTP call.
func (f *Fake) EmitCapture(t *testing.T, orderID string, fee int64) (body []byte, sig string)
```

`failNext` is what makes P4 (Postgres down) and the order-creation-failure path
testable without chaos engineering. `EmitCapture` signing with the test webhook
secret is what makes the whole webhook suite possible offline.

Also record a **real** Razorpay test-mode payload once and commit it to
`testdata/razorpay/payment_captured.json`. Your hand-written fake will drift from
reality; a golden file from the actual provider is what catches that.

---

## 6. Testing FFmpeg

Do not mock FFmpeg. The bugs are in the flags, and a mock asserts your flags are
what you wrote, not that they work.

Commit a 10-second clip (~500 KB):

```bash
ffmpeg -f lavfi -i testsrc=duration=10:size=1920x1080:rate=24 \
       -f lavfi -i sine=frequency=440:duration=10 \
       -c:v libx264 -c:a aac -shortest testdata/sample_1080p.mp4
```

Plus fixtures for the edge cases: `sample_480p.mp4` (ladder truncation),
`sample_portrait.mp4`, `sample_rotated.mp4`, `sample_audio_only.m4a`,
`sample_prores_10bit.mov` (the yuv420p case), `corrupt.mp4` (random bytes with a
video extension).

Assertions run through `ffprobe`, not by eyeballing files:

```go
func assertKeyframesAligned(t *testing.T, outDir string, rungs []string) {
    t.Helper()
    var reference []float64
    for _, r := range rungs {
        ts := keyframeTimestamps(t, filepath.Join(outDir, r, "index.m3u8"))
        if reference == nil { reference = ts; continue }
        require.Equal(t, reference, ts, "rung %s keyframes diverge from reference", r)
    }
}
```

CI needs FFmpeg installed. Add it to the workflow and to `Dockerfile.transcoder`,
and pin the major version — output can differ subtly across releases and a test
that fails only on the CI runner is a miserable afternoon.

---

## 7. End-to-end

Five tests, no more. Full `docker compose up`, real HTTP, the fake gateway.

| # | Scenario |
| --- | --- |
| **E2E-1** | Register → become creator → create campaign → upload pitch → transcode → submit → admin approves → live |
| **E2E-2** | Two backers pledge → webhooks → deadline passes → campaign FUNDED → ledger balances |
| **E2E-3** | Campaign misses goal → FAILED → every pledge refunded → escrow zero |
| **E2E-4** | Funded campaign → upload film → transcode → release → backer plays HLS → non-backer 403 |
| **E2E-5** | Creator requests payout → admin approves → marks paid → pledges SETTLED → escrow zero |

These are slow (minutes). Run on CI and before a demo, not on every save. They
exist to catch wiring mistakes — a route registered on the wrong group, a
consumer subscribed to the wrong topic — that unit and integration tests
structurally cannot see.

E2E-1 through E2E-5 together *are* the demo script. Record them.

---

## 8. What not to test

Skipping these deliberately is as much a strategy as writing the others.

| Don't test | Why |
| --- | --- |
| Gin's routing | it's a library with its own tests |
| That `pgx` executes SQL | same |
| Getters and setters | zero information |
| That a mock was called | tests your test, not your code |
| Exact log strings | brittle; assert the *level* and *key fields* if you must |
| Third-party JSON serialisation | same |
| Every branch, for coverage's sake | 100% coverage of trivial code and 0% of the ledger is worse than the reverse |

**Coverage target: ~70% overall, ~95% on `pledge/`, `outbox/` and
`media/transcode/`.** Uniform coverage targets push effort toward whatever is
easiest to test, which is exactly the code that least needs it.

---

## 9. CI

```yaml
jobs:
  fast:                  # every push, ~90 s
    - go vet ./...
    - golangci-lint run
    - go test ./... -short -race
    - govulncheck ./...
    - gosec ./...

  integration:           # every PR, ~6 min
    services: [docker]
    - apt-get install -y ffmpeg
    - go test ./... -race -tags=integration -timeout 15m

  e2e:                   # main + nightly, ~12 min
    - docker compose -f deploy/docker-compose.yml up -d --wait
    - go test ./e2e/... -tags=e2e -timeout 20m
```

`fast` must stay under two minutes or people stop waiting for it and start
merging on red.

---

## 10. Habits worth forming

1. **When you fix a bug, write the test first.** Watch it fail, then fix. A bug
   without a regression test comes back.
2. **Name tests as behaviour:** `TestWebhook_DuplicateEvent_DoesNotDoubleCredit`,
   not `TestHandleWebhook2`.
3. **`t.Parallel()` on unit tests, never on integration tests** that share a
   truncated database — they'll clobber each other in ways that look like flakes.
4. **Use `require` for preconditions, `assert` for the actual assertions.** A
   test that continues after its setup failed produces confusing output.
5. **Test the error path.** The happy path is what you built; the error path is
   what ships broken.
6. **`go.uber.org/goleak` in `TestMain`.** Leaked goroutines in a consumer or a
   worker pool are invisible until production, and one line catches them.
