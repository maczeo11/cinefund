# 11 — Caching (Redis)

Redis does five distinct jobs here. Keeping them mentally separate matters,
because they have different failure tolerances: losing a cache entry is free,
losing a rate-limit counter is a small security gap, and losing a distributed
lock can corrupt data.

> **Status note (2026-08-06).** Cache-miss fallbacks that read from "Mongo" now
> read from Postgres (the two-store split was reversed — 
> [ADR-0010](DECISIONS/ADR-0010-postgres-only.md)). The key schemes, TTLs and
> stampede handling are unchanged.

| Job | Loss tolerance | Section |
| --- | --- | --- |
| Response cache | free | §2–4 |
| Rate limiting | degraded security | [12](12-RATE-LIMITING.md) |
| Webhook idempotency fast path | free (Postgres is the guarantee) | [06](06-PAYMENTS-RAZORPAY.md) |
| Distributed locks | **can corrupt** | §6 |
| Leaderboards / trending | free (rebuildable) | §7 |

---

## 1. Key naming

```
cf:{version}:{domain}:{identifier}[:{qualifier}]
```

| Key | TTL | Contents |
| --- | --- | --- |
| `cf:v1:campaign:{slug}` | 60 s | campaign page JSON, **without** funding or viewer blocks |
| `cf:v1:campaign:list:{filter_hash}:{cursor}` | 30 s | listing page |
| `cf:v1:film:{slug}` | 300 s | film metadata |
| `cf:v1:film:list:{filter_hash}:{cursor}` | 60 s | catalog page |
| `cf:v1:user:{id}:summary` | 300 s | display name, avatar — for denormalising into comments |
| `cf:v1:tiers:{campaign_id}` | 60 s | tier definitions (immutable once LIVE, so this is nearly free) |
| `cf:v1:trending` | — | ZSET, rebuilt every 5 min |
| `cf:v1:idem:wh:{event_id}` | 24 h | webhook guard |
| `cf:v1:rl:{scope}:{key}` | window | rate limit bucket |
| `cf:v1:lock:{resource}` | ≤ 60 s | distributed lock |
| `cf:v1:jwt:deny:{jti}` | token TTL | logout denylist |

The `v1` segment is a **global cache-buster**. When a response shape changes,
bump it in config and every cache entry is orphaned instantly — no scanning, no
`FLUSHDB`, no stale-shape deserialisation errors. It costs one string in a key
and it is the cheapest insurance in this document.

**Never `KEYS` in application code.** It's O(n) and blocks the single-threaded
server. If you need to enumerate, use `SCAN` with a cursor — but the design above
means you never need to.

---

## 2. What gets cached, and what must not

| Data | Cached? | Why |
| --- | --- | --- |
| Campaign static content (title, synopsis, tiers) | ✅ 60 s | changes rarely, read constantly |
| **`funding.raised_amount`** | ❌ | a backer must see their own pledge immediately |
| Listing pages | ✅ 30 s | staleness is invisible at list granularity |
| Film metadata | ✅ 300 s | near-immutable after release |
| **Entitlement checks** | ❌ **never** | a revoked entitlement must take effect on the next request |
| **Playback URLs** | ❌ | signed, short-TTL, per-user |
| User's own pledges | ❌ | the user's own money; always authoritative |
| Public user summaries | ✅ 300 s | a stale display name for 5 minutes is fine |
| Trending scores | ✅ | derived, approximate by nature |

**The rule for the whole table:** cache anything a stale answer merely makes
slightly wrong. Never cache anything a stale answer makes *unsafe* or *misleading
about the user's own money*.

The campaign detail endpoint is the interesting case — its response is
assembled from a cached part and a live part:

```go
func (s *Service) CampaignDetail(ctx context.Context, slug string, viewer *Actor) (*Detail, error) {
    page, err := s.cache.GetOrLoad(ctx, "cf:v1:campaign:"+slug, 60*time.Second,
        func(ctx context.Context) (*CampaignPage, error) {
            return s.mongo.GetCampaignPage(ctx, slug)     // static content
        })
    if err != nil { return nil, err }

    funding, err := s.pg.GetCampaignFunding(ctx, page.ID)  // ALWAYS live
    if err != nil { return nil, err }

    d := &Detail{CampaignPage: page, Funding: funding}
    if viewer != nil {
        d.Viewer = s.viewerBlock(ctx, page.ID, viewer.UserID)   // also live
        d.Pitch  = s.signPitchURLs(ctx, page)                   // per-request signature
    }
    return d, nil
}
```

One indexed Postgres read on the hottest page in the product, in exchange for a
funding number that is never wrong. Correct trade, and it's a good thing to be
able to justify out loud.

---

## 3. Invalidation

Two mechanisms, used together.

**Delete on write.** The service that changes a campaign deletes its keys in the
same call:

```go
func (s *Service) publishCampaign(ctx context.Context, id uuid.UUID) error {
    if err := s.repo.Publish(ctx, id); err != nil { return err }
    s.cache.Del(ctx, "cf:v1:campaign:"+slug, "cf:v1:tiers:"+id.String())
    return nil
}
```

**Short TTL as the backstop.** Delete-on-write misses the paths you forget:
another process wrote it, a projector updated Mongo, a manual DB fix. A 60-second
TTL bounds every one of those to a minute.

Do not build tag-based or dependency-graph invalidation. It's a lot of machinery
to make a 60-second window into a 0-second window, and the extra correctness is
rarely worth the extra failure modes.

**Do not delete cache keys from a Kafka consumer as the primary mechanism.** By
the time the event lands, the TTL has usually expired anyway, and it couples your
cache correctness to broker health.

---

## 4. Stampede protection

A popular campaign's cache entry expires. 500 concurrent requests all miss, all
query Mongo, all write the same value back. Mongo sees a 500× spike from one
expiry.

Single-flight, in-process plus cross-process:

```go
func (c *Cache) GetOrLoad[T any](ctx context.Context, key string, ttl time.Duration,
    load func(context.Context) (T, error)) (T, error) {

    if v, ok := c.get[T](ctx, key); ok { return v, nil }

    // In-process: collapses concurrent misses within one API replica.
    v, err, _ := c.sf.Do(key, func() (any, error) {
        if v, ok := c.get[T](ctx, key); ok { return v, nil }   // recheck after waiting

        // Cross-process: only one replica repopulates.
        lockKey := key + ":lock"
        ok, _ := c.rdb.SetNX(ctx, lockKey, "1", 5*time.Second).Result()
        if !ok {
            time.Sleep(50 * time.Millisecond)
            if v, ok := c.get[T](ctx, key); ok { return v, nil }
            return load(ctx)          // still cold — just load; better slow than wrong
        }
        defer c.rdb.Del(ctx, lockKey)

        v, err := load(ctx)
        if err != nil { return nil, err }
        c.set(ctx, key, v, jitter(ttl))
        return v, nil
    })
    return v.(T), err
}
```

Two details that matter:

- **`golang.org/x/sync/singleflight` handles the same-process case for free** and
  is the bigger win — most of your concurrency is inside one replica.
- **Jitter the TTL:** `ttl ± 10%`. Without it, everything cached during a traffic
  spike expires simultaneously and you've built a synchronised stampede
  generator. One line, and it prevents a whole failure mode.

```go
func jitter(d time.Duration) time.Duration {
    delta := float64(d) * 0.1
    return d + time.Duration(rand.Float64()*2*delta-delta)
}
```

---

## 5. Fail-open

```go
func (c *Cache) get(ctx context.Context, key string) ([]byte, bool) {
    ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
    defer cancel()
    b, err := c.rdb.Get(ctx, key).Bytes()
    if err != nil {
        if !errors.Is(err, redis.Nil) {
            c.metrics.CacheErrors.Inc()
            c.log.Warn("cache read failed", "key", key, "error", err)
        }
        return nil, false          // treated as a miss
    }
    return b, true
}
```

A cache error is a **miss**, never an error to the caller. And the 50 ms timeout
is essential: without it, a hung Redis makes every request hang for the client's
default timeout, which turns a cache outage into a full outage. The whole point
of a cache is that you can live without it — make the code actually able to.

Writes are best-effort too: log and continue.

---

## 6. Distributed locks

The one Redis use where loss can corrupt. Used for: the deadline sweep, the
reconciliation job, cache repopulation.

```go
func (l *Locker) Acquire(ctx context.Context, key string, ttl time.Duration) (*Lock, error) {
    token := uuid.NewString()                     // unique per acquisition
    ok, err := l.rdb.SetNX(ctx, "cf:v1:lock:"+key, token, ttl).Result()
    if err != nil { return nil, err }
    if !ok       { return nil, ErrLockHeld }
    return &Lock{key: key, token: token, rdb: l.rdb}, nil
}

// Release must be atomic: check the token and delete in one operation.
var releaseScript = redis.NewScript(`
    if redis.call("GET", KEYS[1]) == ARGV[1] then
        return redis.call("DEL", KEYS[1])
    end
    return 0`)

func (l *Lock) Release(ctx context.Context) error {
    return releaseScript.Run(ctx, l.rdb, []string{"cf:v1:lock:" + l.key}, l.token).Err()
}
```

**Why the token, and why the script.** Process A acquires with a 60 s TTL, then
stalls (GC pause, slow disk) for 70 s. The lock expires. Process B acquires it.
Process A wakes and calls `DEL` — deleting *B's* lock. Now two processes hold it.

The token makes A's release a no-op because the stored value is B's token. The
Lua script makes the check-and-delete atomic, because a `GET` followed by a `DEL`
from Go has exactly the same race in miniature.

**This is not Redlock and does not pretend to be.** Single-node Redis locks are
not safe across a Redis failover. For CineFund's uses that is acceptable — the
worst case for a duplicated deadline sweep is a second attempt that finds the
campaign already transitioned and does nothing, because **the underlying
operations are idempotent and guarded by `FOR UPDATE SKIP LOCKED` in Postgres.**

That's the real answer: the Redis lock is an *optimisation* that stops N replicas
doing redundant work. Correctness comes from Postgres. Never let a Redis lock be
the only thing standing between you and double-processing money.

---

## 7. Trending

A sorted set, recomputed every 5 minutes by the scheduler. Not per-request.

```
score = 0.5 * log10(1 + raised_amount/100000)
      + 0.3 * (backers_last_24h)
      + 0.2 * (views_last_24h / 100)
      - 0.4 * days_since_published
```

```go
rdb.ZAdd(ctx, "cf:v1:trending", members...)
rdb.Expire(ctx, "cf:v1:trending", 10*time.Minute)     // stale-detection safety
// read:
rdb.ZRevRange(ctx, "cf:v1:trending", 0, 19)
```

The `log10` on raised amount stops one ₹10,00,000 campaign permanently occupying
the top slot; the recency penalty keeps the list moving. If the ZSET is missing
(Redis restarted), fall back to `ORDER BY raised_amount DESC` from Mongo and let
the next scheduler tick rebuild it.

---

## 8. Serialisation & config

**JSON**, not gob or msgpack. Debuggability wins: `redis-cli GET cf:v1:campaign:x`
returning readable JSON is worth more during an incident than the ~30% size
saving. Revisit if payloads get large; they won't here.

Compress values over 8 KB with gzip and prefix the stored bytes with a one-byte
marker so the reader knows which path to take.

```
REDIS_URL=redis://localhost:6379/0
REDIS_POOL_SIZE=20                 # ~ 2 × GOMAXPROCS
REDIS_MIN_IDLE=5
REDIS_DIAL_TIMEOUT=2s
REDIS_READ_TIMEOUT=50ms            # short: a cache read must never be the slow path
REDIS_WRITE_TIMEOUT=50ms
CACHE_VERSION=v1
CACHE_ENABLED=true                 # a kill switch you will be glad to have
```

`maxmemory-policy allkeys-lru` on the cache instance. If you later co-locate rate
limiting and locks on the same instance, switch to `volatile-lru` so keys without
a TTL are never evicted — or better, **run a second Redis instance** and keep
evictable cache separate from must-not-evict locks and counters. On one small
server, one instance with `volatile-lru` is a reasonable compromise; document
which you chose.

---

## 9. Metrics

```
cache_requests_total{key_prefix, result="hit|miss|error"}
cache_load_duration_seconds{key_prefix}
cache_stampede_waits_total{key_prefix}
redis_command_duration_seconds{command}
lock_acquisitions_total{resource, result="acquired|held|error"}
```

Hit rate below ~70% on `campaign` or `film` prefixes means the TTL is too short
or the key is too specific (usually the filter hash including a param that
doesn't affect the result — normalise the filter before hashing).

---

## 10. Tests

| # | Scenario | Assertion |
| --- | --- | --- |
| C1 | Two loads inside the TTL | source called once |
| C2 | Write then read | cache deleted, fresh value returned |
| C3 | Redis unreachable | every request still succeeds, from source |
| C4 | 100 concurrent misses on one key | source called once (singleflight) |
| C5 | Redis latency injected at 500 ms | requests complete; timeout fires; miss path taken |
| C6 | Lock held by A, B tries | B gets `ErrLockHeld` |
| C7 | A's lock expires, B acquires, A releases | B still holds it (token check) |
| C8 | `CACHE_VERSION` bumped | zero hits on old keys, no deserialisation errors |
| C9 | Entitlement revoked | next playback request is denied (proves it isn't cached) |
