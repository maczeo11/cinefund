# 19 — Performance

Load testing, the bottlenecks you'll find, profiling, and the capacity model.

This document has an unusual property: **the results section is meant to be
filled in with your numbers.** A performance doc with predictions and no
measurements is worth nothing. One with before/after numbers is the strongest
single artefact in the repository.

---

## 1. Why this exists

Most projects claim "built X with Y". Very few can say "I measured it, found the
bottleneck, fixed it, and here are the numbers." The second sentence is the one
that survives a skeptical interviewer, because it is checkable and because it
cannot be produced without having actually run the system under load.

It is also the fastest route to a resume bullet containing a number, which is the
difference between a line that gets read and one that gets skimmed.

---

## 2. Tooling

**k6** — scriptable, low overhead, good percentile output, single binary.

```bash
docker run --rm -i --network host grafana/k6 run - < scripts/load/pledge.js
```

Alternatives if you prefer: `vegeta` (simpler, constant-rate, great for latency
distributions), `bombardier` (fastest to start, least scriptable). Don't use
`ab` — it's single-threaded and will bottleneck before your server does, which
produces confidently wrong numbers.

---

## 3. Scenarios

Four. Each isolates one thing.

### L1 — Campaign detail read (the hot path)

The page that converts. Hybrid read: Mongo for static content, Postgres for
funding.

```js
// scripts/load/campaign_read.js
import http from 'k6/http';
import { check } from 'k6';

export const options = {
  stages: [
    { duration: '30s', target: 50 },
    { duration: '2m',  target: 200 },
    { duration: '30s', target: 0 },
  ],
  thresholds: {
    http_req_duration: ['p(95)<200', 'p(99)<500'],
    http_req_failed:   ['rate<0.01'],
  },
};

const SLUGS = JSON.parse(open('./slugs.json'));

export default function () {
  const slug = SLUGS[Math.floor(Math.random() * SLUGS.length)];
  const res = http.get(`http://localhost:8080/api/v1/campaigns/${slug}`);
  check(res, { 'status 200': (r) => r.status === 200 });
}
```

**Randomise the slug.** Hitting one campaign repeatedly measures your Redis cache,
not your system — you'll get a beautiful number that means nothing. Seed ~200
campaigns and pick randomly.

### L2 — Pledge creation (the write path)

The interesting one. Row locks, an external call, a transaction.

Run it two ways, and the difference is the whole point:

| Variant | What it isolates |
| --- | --- |
| **spread** — 200 campaigns, random tier | throughput ceiling of the general path |
| **contended** — all VUs pledging to *one* limited tier | `FOR UPDATE` serialisation |

The contended variant is where you'll find the real bottleneck.

### L3 — Webhook ingestion

Signed payloads at increasing rate. **Assert correctness, not just latency:**
after the run, `raised_amount` must equal exactly the number of distinct events
processed. A load test on a money path that doesn't verify the money is a
throughput benchmark, not a test.

### L4 — Playlist serving

`GET /films/{id}/hls/master.m3u8` under load. This is the one endpoint where the
API is on the playback path, and it presigns ~250 URLs per variant request.

---

## 4. Method

Getting this wrong produces numbers that mean nothing, so:

1. **Seed realistically.** 200 campaigns, 2,000 users, 5,000 pledges. An empty
   database gives you index-free numbers that collapse the moment there's data.
2. **Warm up.** Discard the first 30 seconds. Go's JIT-less runtime is fine, but
   connection pools, prepared statements, and caches all need to fill.
3. **Change one thing at a time.** Two simultaneous fixes and you learn nothing
   about either.
4. **Record the environment** — CPU count, RAM, Docker limits, Postgres
   `max_connections`, pool sizes, whether the load generator shares the host. A
   number without its environment is not reproducible.
5. **Watch the load generator.** If k6's own CPU is pegged, you're measuring k6.
6. **Re-run correctness under load.** P2 (50 concurrent duplicate webhooks) must
   still pass at peak RPS. A system that's fast and wrong is worse than slow and
   right.

---

## 5. The bottlenecks you will find

Predictions, in the order they'll show up. Confirm or refute each with evidence —
being wrong about one of these is a *better* devlog entry than being right.

### B1 — Razorpay call inside the tier lock

**Symptom:** L2-contended throughput plateaus around 3–5 RPS regardless of VU
count. p99 climbs linearly with concurrency.

**Cause:** if `CreateOrder` is inside the transaction holding `FOR UPDATE` on the
tier, every checkout on that campaign serialises behind a ~300 ms network call.
Ceiling is `1 / 0.3s ≈ 3 RPS`, forever.

**Fix:** commit the pledge insert, then create the order, then attach it —
exactly as [06 §2](06-PAYMENTS-RAZORPAY.md#2-order-creation) specifies. The
design already says this; the load test is what *proves* it mattered.

**Expected:** ~3 RPS → 60+ RPS on the contended path.

### B2 — Connection pool exhaustion

**Symptom:** latency is flat, then a cliff. Logs show
`timeout: context deadline exceeded` on acquire.

**Cause:** `POSTGRES_MAX_CONNS` too low, or a handler holding a connection across
an external call.

**Fix:** size the pool to roughly `4 × cores`, and audit for connections held
across network I/O. Set `pool_max_conn_lifetime` so connections recycle.

### B3 — The N+1 in campaign listing

**Symptom:** L1 p95 grows with page size.

**Cause:** fetching tiers or creator info per campaign in a loop.

**Fix:** the listing projection in Mongo already denormalises this — the bug is
reaching past it. `EXPLAIN` will show it immediately.

### B4 — Presigning cost on playlists

**Symptom:** L4 p99 in the hundreds of milliseconds; API CPU dominated by
`crypto/hmac`.

**Cause:** SigV4 for ~250 segments per request, every request, uncached.

**Fix:** cache the *signed variant playlist* keyed by `(asset, rung, ttl_bucket)`
where `ttl_bucket = expiry rounded down to 5 minutes`. Viewers within the same
bucket share a playlist. Correct, because the signature is not per-viewer — the
**authorisation** is, and that happens before the cache lookup.

### B5 — Redis round trips per request

**Symptom:** a flat ~2–4 ms floor on every endpoint.

**Cause:** three or four sequential Redis calls — rate limit, cache, denylist.

**Fix:** pipeline the independent ones. Measure first; this is often not worth
fixing.

---

## 6. Results

> Fill this in. Commit the k6 output JSON to `docs/perf/` alongside it.

### Environment

```
CPU / RAM        :
Docker limits    :
Postgres         : version, max_connections, pool size
Load generator   : on-host / separate
Commit           :
Date             :
```

### L1 — Campaign detail

| Metric | Before | After | Change |
| --- | --- | --- | --- |
| RPS sustained | | | |
| p50 | | | |
| p95 | | | |
| p99 | | | |
| error rate | | | |

**Bottleneck found:**
**Fix applied:**
**Evidence:** *(EXPLAIN output, pprof profile, or the metric that moved)*

### L2 — Pledge creation (contended)

| Metric | Before | After | Change |
| --- | --- | --- | --- |
| RPS sustained | | | |
| p95 | | | |
| p99 | | | |
| correctness (P2 at peak) | | | |

**Bottleneck found:**
**Fix applied:**
**Evidence:**

### L3 — Webhook ingestion

| Metric | Before | After |
| --- | --- | --- |
| events/sec | | |
| p99 | | |
| `raised_amount` correct | | |

### L4 — Playlist serving

| Metric | Before | After |
| --- | --- | --- |
| RPS | | |
| p99 | | |
| API CPU % | | |

---

## 7. Query performance

`EXPLAIN (ANALYZE, BUFFERS)` on the three hottest queries. Record the plan, the
index used, and why that index exists.

| Query | Index | Why |
| --- | --- | --- |
| deadline sweep | `idx_campaigns_status_deadline` (partial, `WHERE status='LIVE'`) | runs every 60 s forever; must touch only live campaigns, not every campaign ever created |
| outbox claim | `idx_outbox_unpublished` (partial, `WHERE published_at IS NULL`) | the table grows without bound; the query only ever wants a handful of rows |
| entitlement check | `idx_entitlements_lookup` (partial, `WHERE revoked_at IS NULL`) | on the playback path, uncached, must be a single index lookup |
| pledge reconciliation | `idx_pledges_stale` (partial, `WHERE status='CREATED'`) | sweeps every 15 min; only stale rows matter |

**Record the actual plans.** "Index Scan using idx_campaigns_status_deadline
(cost=0.28..8.31 rows=3)" is evidence. "We added an index" is a claim.

Watch for `Seq Scan` on anything over ~10k rows, `Rows Removed by Filter` in the
thousands, and a planner row estimate more than 10× off the actual — the last one
usually means `ANALYZE` hasn't run.

---

## 8. Profiling

`net/http/pprof` on the internal listener only, never the public one.

```bash
go tool pprof -http=:8081 http://localhost:6060/debug/pprof/profile?seconds=30
go tool pprof -http=:8081 http://localhost:6060/debug/pprof/heap
curl 'http://localhost:6060/debug/pprof/mutex?debug=1'
```

Take profiles **during** L2, not at idle. An idle profile shows you the runtime.

What to look for:

| Profile | Suspect |
| --- | --- |
| CPU | JSON marshalling, HMAC signing, bcrypt/argon2 on the request path |
| Heap | per-request allocations in middleware; slices grown without capacity hints |
| Mutex | contention in the worker registry or a cache map |
| Block | the actual smoking gun for lock-held-across-network-call |
| Goroutine | leaks — count should be stable after load stops |

**One fix, documented, is the deliverable.** Not a survey. "Profiled under load,
found `X`, changed `Y`, allocations dropped `Z%`" is the sentence you want.

Set `runtime.SetMutexProfileFraction(5)` and `SetBlockProfileRate(...)` behind a
config flag — they cost a little and are off by default.

---

## 9. Transcoding capacity

The one part of this system that scales with content rather than users, and the
model is simple enough to derive honestly.

**Measure `realtime_factor`** — FFmpeg's `speed=2.18x` — per rung, on real
content. Then:

```
encode_time(rung)  = content_duration / realtime_factor(rung)
encode_time(job)   = Σ over rungs        (sequential within a job)
job_capacity       = TRANSCODE_CONCURRENCY per worker
throughput(worker) = job_capacity / encode_time(job)
workers_needed     = peak_uploads_per_hour / (throughput × 3600)
```

Worked example — fill in your own measurements:

```
content_duration        : 600 s (10-minute short)
realtime_factor  720p   : 2.1x  → 286 s
                 480p   : 4.8x  → 125 s
                 360p   : 8.0x  →  75 s
encode_time(job)        : 486 s  ≈ 8.1 min
TRANSCODE_CONCURRENCY   : 2
throughput per worker   : 2 / 486 s = 14.8 jobs/hour
peak uploads/hour       : 30
workers needed          : 30 / 14.8 ≈ 2.03 → 3 with headroom
```

**The leading indicator is `realtime_factor`, not queue depth.** When the p50
drops below ~1.0 you cannot keep up with real-time ingestion and the queue will
grow without bound — but queue depth only tells you *after* it has already
happened. Alert on the factor.

---

## 10. What would change at 100× scale

The question a senior interviewer asks, worth having a real answer to.

| Load today | At 100× | What changes |
| --- | --- | --- |
| ~50 campaigns, ~1k pledges/day | 5k campaigns, 100k pledges/day | Postgres still fine. Partition `payment_events` and `outbox` by month. Read replicas for the catalog. |
| 1 dispatcher | outbox lag becomes visible | `SKIP LOCKED` already allows N replicas — just run more. Batch size up, `LISTEN/NOTIFY` to cut the poll floor. |
| 3 transcoders | ~200 uploads/hour | Horizontal, driven by the capacity model above. GPU encoding (NVENC) at ~10× realtime, at some quality cost — measure before committing. |
| Playlist rewriter on the API | playback dominates traffic | Move to **CDN signed cookies** ([ADR-0008](DECISIONS/ADR-0008-hls-access-control.md) option C). The API leaves the playback path entirely. |
| Mongo text search | search becomes the bottleneck | Elasticsearch or Atlas Search, fed from the same Kafka topics — the projector pattern already makes this a new consumer, not a rewrite. |
| Single Redis | limiter + cache + locks contend | Split into three instances by role. Locks move to Postgres advisory locks or a real Redlock. |
| One API replica | gRPC registry is per-replica | Redis pub/sub fan-out for cancel commands ([13 §5](13-GRPC-CONTROL-PLANE.md#5-server-side)). |
| Modular monolith | team grows past ~8 | `pledge` and `media` split out first — they have the cleanest boundaries and the most different scaling profiles. |

The honest summary: **the first thing to break is not the database.** It's
transcoding CPU and CDN egress, because both scale with video volume rather than
request volume. That's the answer worth giving, because most people reach for
"shard the database" and it's the wrong instinct here.
