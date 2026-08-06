# 16 — Build Order

Four groups, built as a **vertical slice first**. Each group ends with something
demoable, and group C is a complete, resume-true project on its own.

---

## 1. The sequencing decision

The obvious order is dependency order: infrastructure, auth, campaigns, payments,
uploads, transcoding, playback. It is the wrong order here, for two reasons.

**FFmpeg is the largest unknown and ~20% of the budget.** Discovering in week 9
that the pipeline is harder than expected is unrecoverable. Discovering it in
week 2 is a schedule adjustment.

**Dependency order leaves you with N half-built layers at every point.** If the
calendar runs out — and calendars do — you want a working system with fewer
features, not a broken system with all of them.

So: **build the video path end to end first, with no auth and no money.** Upload
a file, transcode it, play it in a browser. Then layer the money path on top of a
pipeline you already trust.

```
Group A   video spine        upload → transcode → HLS → plays in a browser
Group B   money spine        auth → campaigns → payments → ledger → outbox → Kafka
Group C   completion         entitlements, gRPC, tests, load test, fault injection
                             ◄── COMPLETE, RESUME-TRUE PROJECT ENDS HERE
Group D   depth              read model, caching, DLQ, reconciliation, observability, ops
```

---

## 2. Calendar

Assumes ~3.5 h/day, with PRAJNA handing over in the second week of August.

| Week | Dates | Hours | Cumulative | Milestone |
| --- | --- | --- | --- | --- |
| W1 | Aug 3–9 | 16 | 16 | PRAJNA still running |
| W2 | Aug 10–16 | 18 | 34 | PRAJNA handover ~Aug 14 |
| W3 | Aug 17–23 | 22 | 56 | **A done — video plays** |
| W4 | Aug 24–30 | 22 | 78 | |
| W5 | Aug 31–Sep 6 | 22 | 100 | |
| W6 | Sep 7–13 | 22 | 122 | **B done — money works** |
| W7 | Sep 14–20 | 22 | 144 | |
| W8 | Sep 21–27 | 22 | 166 | **C done ≈ Sept 24 — interviewable** |
| W9 | Sep 28–Oct 4 | 22 | 188 | |
| W10 | Oct 5–11 | 22 | 210 | |
| W11 | Oct 12–18 | 22 | 232 | **D done ≈ Oct 14** |

**Total: ~224 h.** Group C is the checkpoint that matters — after it, every claim
on your resume is literally true and demonstrable. Group D happens while you're
interviewing, which is better than holding the project back until it's "done."

---

## 3. What to hand-write

The ✋/🤖 column is not about difficulty. It's about **which bugs a test can
catch.**

| | AI-assist 🤖 | Hand-write ✋ |
| --- | --- | --- |
| Knowledge type | declarative — flags, config, idioms | structural — where the transaction boundary goes |
| Bug visibility | fails loudly; a test catches it | invisible in review, surfaces in an incident |
| Interview question | "what does `-sc_threshold 0` do?" | "walk me through your webhook handler" |

FFmpeg is the first column: get a flag wrong and test M9 fails. The webhook
handler is the second: AI produces working code missing the `redis.Del` on the
failure path, every test passes, and you lose an event the first time Postgres
hiccups.

### The irreducible core — ~480 lines

For scale, Group A+B+C is roughly:

| | Lines |
| --- | --- |
| `internal/platform/*` | ~2,400 |
| domain packages (identity, campaign, pledge, media, playback, outbox) | ~3,400 |
| `cmd/*` — mostly wiring | ~600 |
| SQL migrations | ~600 |
| **Go you actually write** | **~6,400** |
| generated protobuf (committed, not written) | ~1,200 |
| tests | ~2,000 |
| **repo total** | **~9,600** |

Group D takes the repo to ~12,500.

But the number that matters is smaller: of those 6,400 lines, **maybe 2,500
involve a real decision.** The rest is six domains sharing one five-file shape —
struct definitions, `rows.Scan` boilerplate, `if err != nil`, bind-validate-call-
respond. Go is verbose by design.

Against that 2,500, the 480-line core below is **~20% of the code where you make
a choice, and ~90% of the interview risk.**

| File | ~Lines | Why it must be yours |
| --- | --- | --- |
| `pledge/webhook.go` — `HandleWebhook` | 80 | the two-layer guard, and `Del`-on-failure |
| `pledge/service.go` — `applyCapture`, `CreatePledge` | 120 | what's inside the transaction and what isn't |
| `pledge/ledger.go` — `RecordPledgeCapture`, `RecordRefund` | 100 | entry directions; idempotent `IsUnique → nil` |
| `outbox/dispatcher.go` — `tick()` | 60 | `SKIP LOCKED`, produce-then-mark ordering |
| `media/transcode/worker.go` — lease, heartbeat, abort | 80 | the `worker_id` filter that aborts a stolen job |
| `media/transcode/args.go` — the FFmpeg arg builder | 40 | not for the code — for the decisions |

AI-assist everything else.

---

# GROUP A — Video spine (W1–W3, 45 h)

No auth. No payments. Hardcoded owner id. The goal is a file that goes in and a
video that comes out.

## A0 — Skeleton & infrastructure · 5 h · 🤖

Repo, `go.mod`, Makefile, `deploy/docker-compose.yml` with Postgres, Mongo
(**replica set**), Redis, Kafka (KRaft), MinIO + `mc` init. `cmd/migrate`.
Migrations 0001–0015. Mongo indexes. Kafka topics with the partition counts from
[08 §4](08-EVENTING-OUTBOX-KAFKA.md#4-topics).

```
[ ] `make up` brings all six services up healthy
[ ] `make migrate` applies cleanly; down/up round-trips
[ ] mongosh confirms rs0 is PRIMARY and a change stream opens
[ ] `mc ls local/` shows four buckets; originals and media are both private
[ ] `make down && make up` reproduces everything from scratch
```

**Do not shortcut the Mongo replica set.** Solve it here, not in B6 when you're
also debugging change-stream filters and can't tell which half is broken.

## A1 — Config, logging, errors, health · 3 h · 🤖

`platform/config` (typed, validated at boot), `platform/logger` with redaction,
`platform/errs` with `Kind` → HTTP mapping, `/health/live` and `/health/ready`.

```
[ ] a missing JWT secret exits at boot with a clear message
[ ] every log line carries service, version, trace_id, request_id
[ ] /health/live never touches a dependency (test asserts it with all deps down)
[ ] /health/ready is 503 when Postgres is down, 200 when only Redis is
```

## A2 — Presigned upload · 5 h · 🤖

`objectstore.Store` (S3 + in-memory), presign with pinned method/type/size,
server-generated keys, `POST /complete` with `HEAD` verification, the
internal/browser audience split, `media_assets` in Mongo.

```
[ ] a 500 MB file uploads with the API's RSS flat throughout   ← the point
[ ] S0 the API's credentials cannot GetObject from cinefund-originals
[ ] an unsigned GET on a media key returns 403
[ ] a presign for image/jpeg rejects a video upload
[ ] sanitiseFilename("../../etc/passwd") cannot escape the asset prefix
[ ] a browser-audience URL uses the public endpoint and validates
```

Watch `docker stats` while uploading. A flat memory line is the acceptance
criterion, not a nice-to-have.

**Expect the MinIO endpoint trap** ([10 §6](10-OBJECT-STORAGE.md#the-presigned-url-hostname-trap)).
It will cost an hour. Write the devlog entry.

## A3 — FFmpeg, single rung, called directly · 14 h · 🤖 (args ✋)

**No Kafka yet.** `POST /complete` calls the transcoder in-process. One 480p
rendition. Get a video playing before adding anything else.

ffprobe wrapper, rejection rules, rotation handling, the arg builder, progress
parsing, temp dir lifecycle.

```
[ ] a 10 s sample produces 480p/index.m3u8 + segments that play in VLC
[ ] progress is parsed from -progress pipe:1 and logged
[ ] an audio-only file is REJECTED with no FFmpeg invocation
[ ] a corrupt file is REJECTED, error captured, temp dir cleaned
[ ] SIGTERM on the job kills FFmpeg within 2 s and cleans up
```

Generate the fixture once:

```bash
ffmpeg -f lavfi -i testsrc=duration=10:size=1920x1080:rate=24 \
       -f lavfi -i sine=frequency=440:duration=10 \
       -c:v libx264 -c:a aac -shortest testdata/sample_1080p.mp4
```

## A4 — Full ABR ladder · 12 h · 🤖 (args ✋)

Three rungs (720/480/360 — not five), keyframe alignment, master playlist,
poster frame, deterministic keys, the worker pool semaphore, job leases.

```
[ ] M9  keyframe timestamps IDENTICAL across all rungs   ← the ABR proof
[ ] M2  a 480p source yields exactly 2 rungs, none upscaled
[ ] M3/M4 portrait and rotated sources come out upright
[ ] M13 10-bit ProRes → yuv420p that plays in Chrome
[ ] M7  kill a worker mid-job → another reclaims the lease and finishes
[ ] M8  duplicate dispatch → one job, one set of renditions
```

**Own your flags.** Delete `-sc_threshold 0`, run M9, watch the keyframes
diverge, put it back. Ninety seconds, and you now own that flag in a way reading
cannot provide.

## A5 — Playlist rewriter & browser playback · 6 h · ✋

The rewriter with path validation, duration-scaled segment TTL, an `hls.js` page.

```
[ ] a film plays end to end in a browser with working seek
[ ] throttling the network drops a rung and recovers
[ ] segment URLs outlive the film's runtime
[ ] a crafted "../" line in a playlist is rejected
```

**🎬 Group A demo:** drag a file in, watch it transcode, press play.

---

# GROUP B — Money spine (W3–W6, 67 h)

## B0 — Auth · 5 h · ✋ (core)

**Port from MagicStream**, then add refresh rotation with reuse detection and the
10-second grace window. Argon2id. `authz.CanActOn`.

```
[ ] register/login/refresh/logout round-trip with cookies
[ ] a POST body containing "role":"ADMIN" produces a USER
[ ] replaying a used refresh token burns the family → 401 TOKEN_REUSED
[ ] the grace window accepts a legitimate double-refresh
[ ] login timing is indistinguishable for unknown vs wrong-password
```

## B1 — Rate limiting · 4 h · ✋ (the Lua)

Redis Lua token bucket, layered middleware, `SetTrustedProxies`, per-email login
counter, in-memory fallback.

```
[ ] R5  100 concurrent requests against a limit of 10 → exactly 10 pass  ← the proof
[ ] Redis down → requests still served, fallback counter increments
[ ] a spoofed X-Forwarded-For from an untrusted source is ignored
[ ] 5 failed logins for one email from 50 IPs trips the per-email limit
```

## B2 — Campaigns & state machine · 8 h · 🤖

Create, publish, list, reward tiers, the transition table, the field-editability
matrix, the submit gate. **No admin review queue, no creator profiles** — the
creator publishes directly.

```
[ ] DRAFT → LIVE via the API
[ ] editing goal_amount after publish returns 409
[ ] submit with 3 problems returns all 3 in details.failures
[ ] another creator's DRAFT returns 404, not 403
[ ] every illegal transition rejected by CanTransitionTo, table-tested
```

## B3 — Payments & idempotency · 20 h · ✋

**The most important phase.** Gateway interface + real + fake, order creation,
raw-body capture, HMAC verification, the two-layer guard, capture/failure/refund
application.

```
[ ] P2  the same webhook 50× concurrently → exactly one state change
[ ] P4  webhook during a Postgres outage → 500, applied exactly once on retry
[ ] P5  Redis flushed between deliveries → the constraint catches it
[ ] P6  amount mismatch → 500, alert, no state change
[ ] P7  two concurrent pledges for the last tier slot → one 409, claimed == limit
[ ] P12 order created but AttachOrder failed → webhook finds the pledge via notes
```

`scripts/fake-webhook.sh` from day one — you need to hammer P2 locally without
ngrok. Note `--data-raw`, not `--data`: the latter mangles whitespace and breaks
the HMAC.

## B4 — Ledger · 8 h · ✋

Accounts, transactions, entries, the deferred balance trigger, capture and refund
movements. **Payout ledger accounts exist; the payout HTTP flow does not.**

```
[ ] L5 a deliberately unbalanced insert FAILS at COMMIT   ← write this FIRST
[ ] a capture produces 4 balanced entries
[ ] replaying a capture produces no second transaction
[ ] capture → refund initiated → refund processed leaves campaign escrow at 0
[ ] net = gross − fee exactly, for awkward remainders
```

Write L5 *before* the trigger exists. Watch it fail, add the trigger, watch it
pass. That's how you know the safety net is wired up rather than assumed.

## B5 — Outbox, dispatcher, Kafka · 16 h · ✋

The outbox writer inside service transactions, `cmd/dispatcher` with
`FOR UPDATE SKIP LOCKED`, the consumer loop with dedupe, trace propagation
through Kafka headers. **Retrofit A3's direct call to go through Kafka.**

```
[ ] E1 kill the API between COMMIT and dispatch → published on restart
[ ] E3 crash between produce and UPDATE → delivered twice, one effect
[ ] E4 two dispatcher replicas, 1000 rows → each published exactly once
[ ] E5 the same event twice → one side effect
[ ] E10 one trace spans HTTP → outbox → Kafka → transcoder
```

E4 is the `SKIP LOCKED` proof; E1 is why the pattern exists at all. Both are demo
material.

## B6 — Change stream watcher · 6 h · ✋

`cmd/mediawatcher` with resume-token persistence, the `$match` filter,
`UpdateLookup`, and the catch-up scan.

```
[ ] E8 watcher restarts → resumes from token, no gap, no full replay
[ ] E9 down past the oplog window → ChangeStreamHistoryLost detected, catch-up recovers
[ ] assets stuck in UPLOADED > 5 min are picked up by the sweep
```

**Expect `SetFullDocument(UpdateLookup)` to be the bug.** Without it the `$match`
on `fullDocument.status` never matches and the watcher silently does nothing.

**🎬 Group B demo:** two backers fund a campaign, webhooks land, ledger balances,
events flow to Kafka.

---

# GROUP C — Completion (W6–W8, 41 h)

**◄── The project is complete and resume-true at the end of this group.**

## C0 — Entitlements & playback authorisation · 7 h · ✋

`EARLY_ACCESS` only. The resolver, grants at release, never cached.

```
[ ] a backer watches during the window; a non-backer gets 403 with a reason
[ ] a revoked entitlement takes effect on the very next request
[ ] admin playback allowed and audit-logged
```

## C1 — gRPC control plane · 4 h · 🤖

Two RPCs: `ReportProgress` (with `should_cancel`) and `CheckJobStatus`. Plus
`Connect` for registration. No multi-replica fan-out.

```
[ ] a worker registers and heartbeats
[ ] CancelJob → SIGTERM within 2 s, temp dir cleaned
[ ] the control plane down 5 min → transcoding continues uninterrupted
[ ] 20 simulated workers → no goroutine leak (goleak)
```

## C2 — Concurrency & idempotency test suite · 10 h · ✋

The ~15 tests that justify the architecture. Not all 81 — these:

```
P2 P4 P5 P7 P12    payments under concurrency and failure
L5 L7              ledger balance enforcement, the R6 identity
E1 E3 E4 E5        outbox at-least-once, SKIP LOCKED
M7 M8 M9           transcoder leases, dedupe, keyframe alignment
R5                 rate limiter atomicity
C4                 cache singleflight
```

All with `-race`, all with a `close(start)` channel so goroutines are released
simultaneously. Staggered launches test nothing.

## C3 — Load test → bottleneck → fix → numbers · 12 h · ✋

**Your single best resume bullet.** Full methodology in
[19 — Performance](19-PERFORMANCE.md).

```
[ ] k6 script against POST /pledges and GET /campaigns/{slug}
[ ] baseline p50/p95/p99 recorded and committed
[ ] a real bottleneck identified with evidence, not a guess
[ ] fixed, re-measured, before/after committed to docs/19
[ ] correctness re-verified under load — P2 still passes at peak RPS
```

## C4 — Fault injection & the recording · 8 h · ✋

Scripts in `scripts/chaos/`. Mostly wiring, since the tests already exist.

```
[ ] kill Postgres mid-webhook       → 500, retry applies exactly once
[ ] kill the dispatcher mid-publish → event redelivered on restart
[ ] kill a transcoder mid-job       → reclaimed, finished, no duplicates
[ ] flush Redis                     → cache misses, limiter falls back, nothing breaks
[ ] stop Kafka                      → API serves, outbox_lag climbs, drains on recovery
[ ] a 90-second screen recording of all five
```

**🎬 Group C demo:** the recording. This is what you send.

---

# GROUP D — Depth (W8–W11, 71 h)

Build this while interviewing.

| Phase | h | ✋/🤖 | Done when |
| --- | --- | --- | --- |
| D0 Mongo read model + projector consumer | 10 | 🤖 | listing served from Mongo; detail still reads funding from Postgres |
| D1 Redis caching | 7 | 🤖 | C1–C9; funding numbers provably never cached |
| D2 Retry topics + DLQ + replay | 6 | 🤖 | E6, E7; permanent vs transient classified correctly |
| D3 Reconciliation sweep | 8 | ✋ | R1–R7 run nightly; P13 (missing webhook) self-heals |
| D4 Observability | 8 | 🤖 | one trace across three processes in Jaeger; `outbox_lag_seconds` exposed; one dashboard from committed JSON |
| D5 Ops CLI | 6 | 🤖 | replay outbox, **rebuild the read model from scratch**, force re-transcode |
| D6 `EXPLAIN ANALYZE` on the 3 hot queries | 4 | ✋ | index choices justified in [19](19-PERFORMANCE.md) |
| D7 pprof under load, one fix | 6 | ✋ | a profiled allocation or lock contention found and fixed |
| D8 Capacity model + "100× scale" section | 4 | ✋ | transcoders-per-upload-rate derived from measured `realtime_factor` |
| D9 CI, security checklist, deploy, demo data | 12 | 🤖 | [05 §11](05-AUTH-SECURITY.md#11-pre-launch-security-checklist) fully ticked; CI green on a clean clone |

D5's "rebuild the read model from scratch" is worth singling out. Replaying the
event log to reconstruct Mongo from Postgres is six hours of work that reads as
genuinely senior, because it proves the read model really is derived rather than
just claimed to be.

---

## 4. Working rules

**1. Commit continuously, in small pieces.** This is the anti-slop evidence and
it costs nothing. `feat(pledge): reject pledges within 60s of deadline` beats one
`feat: payments` at week's end. Commit each ADR *before* the code it justifies.

**2. Keep [DEVLOG.md](DEVLOG.md).** Two sentences per dead end. This is what
converts AI-assisted work into defensible work — AI didn't write the entry about
the afternoon you lost to `SetFullDocument`.

**3. Write the test that encodes each bug you hit.** M13 exists because
`format=yuv420p` is easy to omit and the failure looks like "video is broken".
The test is the receipt.

**4. Don't start the next phase with the current one's boxes unticked.**
Half-finished phases compound, and a payment layer built on a shaky transaction
boundary costs more to fix later than to do properly now.

**5. Demo at the end of each group.** Even to nobody. If you can't show it in 60
seconds, it isn't done.

---

## 5. Risk register

| Risk | Likelihood | Impact | Mitigation |
| --- | --- | --- | --- |
| A3/A4 (FFmpeg) overruns | medium | 1–2 weeks | **de-risked by building it first**; one rung playing before touching the ladder; timebox to 32 h then ship 2 rungs |
| Razorpay test-mode friction | medium | days | `scripts/fake-webhook.sh` replaying signed payloads locally from B3 day one |
| Mongo replica set fights you | low | days | solved in A0, not B6 |
| Kafka consumer-group semantics | medium | days | one consumer end to end in B5 before writing a second |
| Placement interviews eat weeks | **high** | 2–3 weeks | **Group C is the checkpoint**; D is explicitly interview-season work |
| Scope creep (a React SPA) | medium | 4+ weeks | non-goals in [00 §7](00-PRODUCT-SPEC.md#7-non-goals-for-v1); the demo page is 3 ugly screens and `hls.js` |

PRAJNA handing over in W2 removes what was previously the largest item here.

**If you stop after Group C** you have: JWT auth with rotation and reuse
detection, a distributed rate limiter proven atomic under concurrency, campaigns
with a real state machine, idempotent payments with a double-entry ledger, a
transactional outbox feeding Kafka, Mongo change streams, presigned uploads, an
FFmpeg HLS pipeline with verified keyframe alignment, authorised playback, a gRPC
control plane, a load test with before/after numbers, and a fault-injection
recording.

Every claim on your resume, demonstrable, by **~24 September**. Group D makes it
better. It does not make it true.
