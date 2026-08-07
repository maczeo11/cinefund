# 16 — Build Order

Scope is locked to the resume: **four claims, done end-to-end and demonstrable.**
Everything else is explicitly out of scope (see Non-goals). A claim counts as
done when it has an HTTP entry point, a live database behind it, and a test that
proves it under the failure it exists for.

**The four resume claims**

1. REST API (Postgres / Redis / Kafka)
2. Webhook idempotency
3. Transactional outbox
4. FFmpeg ABR pipeline

---

## 1. What each claim still needs

### Claim 2 — Webhook idempotency · CORE DONE, HTTP PENDING

Service layer is written and tested offline: gateway interface + fake, two-layer
guard (Redis `SETNX` in front of `uq_provider_event`, `Del`-on-failure), state
machine, amount-match guard, ledger in one transaction. Remaining work is the
route and one live-failure test.

```
[x] P2  50× the same webhook → exactly one state change (offline)
[x] P5  Redis flushed between deliveries → constraint catches it
[x] P6  amount mismatch → 500, no state change
[ ] P4  Postgres outage → 500 → retry → applied exactly once   ← live test
[ ] POST /webhooks/razorpay route
[ ] scripts/fake-webhook.sh — hammer P2 locally without ngrok
```

`scripts/fake-webhook.sh` from day one. `--data-raw`, not `--data`: the latter
mangles whitespace and breaks the HMAC.

### Claim 4 — FFmpeg ABR pipeline · CORE DONE, NEVER RUN

The library is written and unit-tested: probe + rejection rules, a ladder that
never upscales, GOP-aligned args, duration-weighted progress, master playlist
written last, worker with `worker_id` fencing. It has never touched a real file.
Remaining work is one end-to-end run and the enqueue path that creates jobs.

```
[x] M2/M3/M4/M9/M12/M13   unit tests (ladder, args, probe, playlists)
[ ] make sample → one real 480p+HLS transcode that plays
[ ] enqueue path: a media-uploaded consumer creates a transcode job
[ ] worker/runner test; M7 live (kill worker → another reclaims)
```

Generate the fixture once:

```bash
ffmpeg -f lavfi -i testsrc=duration=10:size=1920x1080:rate=24 \
       -f lavfi -i sine=frequency=440:duration=10 \
       -c:v libx264 -c:a aac -shortest testdata/sample_1080p.mp4
```

### Claim 3 — Transactional outbox · CORE DONE, NO DISPATCHER

Outbox table, partial index, and the in-transaction writers (`applyCapture`,
`Succeed`) are done. Remaining work is the dispatcher binary and one consumer
that proves the rows go somewhere.

```
[x] outbox table + in-transaction writers (migration 0013)
[ ] cmd/dispatcher: poll WHERE published_at IS NULL → Kafka → mark published
[ ] a consumer that enqueues a transcode job when media is ready
[ ] E1 kill the API between COMMIT and dispatch → published on restart
```

### Claim 1 — REST API · NOT STARTED

The glue that makes claims 2–4 demonstrable. Minimal by design — no auth, no
rate limiting, no campaign module beyond the pledge flow needs.

```
[ ] POST /campaigns/{id}/pledges
[ ] POST /webhooks/razorpay        (HMAC verify, then HandleWebhook)
[ ] minimal POST /campaigns + GET /campaigns/{id} so a demo has data
[ ] POST /uploads presign + POST /uploads/{id}/complete   (presigned uploads)
[ ] error middleware: errs.Kind → HTTP status in exactly one place
[ ] internal/pledge/gateway/razorpay — the real adapter
```

---

## 2. Non-goals — explicitly NOT built

These are the resume cut. If it isn't one of the four claims, it isn't built.

- **Auth / JWT / refresh rotation** — no login; the demo uses a fixed backer id.
- **Rate limiting** — Redis Lua token bucket (R5) is a docs/12 design, not a claim.
- **Campaign module** — a stub with create/get and reward tiers is enough for
  `POST /pledges` to exist. No publish workflow, no submit gate.
- **Identity module** — users table exists for FKs; no profile flows.
- **gRPC control plane** (Group C1) — the worker already heartbeats to Postgres.
- **Entitlements / playback authorisation** (C0) — not a resume claim.
- **Load test / fault-injection recording** (C3/C4) — replaces Group C.
- **Refund/payout HTTP flows** — ledger movements for refunds exist in the
  domain; no HTTP flow, no payouts worker.
- **Group D entirely** — read model, caching, DLQ, reconciliation,
  observability, ops, performance. None of it is on the resume.

---

## 3. Order

Phase 1 — API routes (Claim 1) — unblocks pledges and the webhook.
Phase 2 — wire `POST /webhooks/razorpay` + fake-webhook script (Claim 2).
Phase 3 — media enqueue + `make sample` + one real transcode (Claim 4).
Phase 4 — `cmd/dispatcher` + one consumer (Claim 3).
Phase 5 — integration suite: P4, E1/E3/E4/E5, M7 live.

**Done** when there are four 60-second demo scripts, each proving one claim.

---

## 4. Working rules

1. **Commit continuously, in small pieces.** `feat(pledge): reject pledges
   within 60s of deadline` beats one `feat: payments` at week's end.
2. **Keep [DEVLOG.md](DEVLOG.md).** Two sentences per dead end. This is what
   converts AI-assisted work into defensible work.
3. **Write the test that encodes each bug you hit.** M13 exists because
   `format=yuv420p` is easy to omit and the failure looks like "video is broken".
   The test is the receipt.
4. **Don't start the next phase with the current one's boxes unticked.**
5. **Demo at the end of each phase.** If you can't show it in 60 seconds, it
   isn't done.

---

## 5. Risk register

| Risk | Likelihood | Impact | Mitigation |
| --- | --- | --- | --- |
| Transcode never run (Claim 4) | medium | resume gap | `make sample` first; one rung playing before the ladder |
| Razorpay test-mode friction | medium | days | `scripts/fake-webhook.sh` replaying signed payloads from phase 2 |
| Kafka consumer semantics | medium | days | one consumer end to end in phase 4 before a second |
| Scope creep | **high** | 4+ weeks | Non-goals above are explicit; if it isn't a claim, refuse |
