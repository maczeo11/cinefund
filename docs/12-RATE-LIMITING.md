
\# 12 — Rate Limiting

A distributed token bucket in Redis, applied in layers.

---

## 1. Why not the in-memory limiter

MagicStream uses a per-process in-memory token bucket. It's correct for one
replica and wrong for N: with 3 API replicas behind a load balancer, a configured
limit of 5/min becomes an effective 15/min, and which limit you get depends on
which replica the LB picked. For a login endpoint that is a real weakness — the
whole point is to slow credential stuffing.

Redis moves the counter to shared state. The cost is one round trip per request
(~0.3 ms on a local network) and a new dependency on the request path — which is
why §5 (fail-open) exists.

---

## 2. Token bucket, in Lua

The algorithm must be **atomic**: read tokens, compute refill, check, decrement.
Doing that with separate `GET`/`SET` calls from Go is a race that lets N
concurrent requests all see the same token count and all pass.

Redis executes a Lua script atomically. This is the canonical use case.

```lua
-- rate_limit.lua
-- KEYS[1] = bucket key
-- ARGV[1] = capacity (burst)
-- ARGV[2] = refill rate (tokens/second)
-- ARGV[3] = now (unix seconds, float)  -- passed in, never TIME, see below
-- ARGV[4] = requested tokens (usually 1)
-- returns { allowed(0|1), remaining, retry_after_seconds }

local capacity   = tonumber(ARGV[1])
local rate       = tonumber(ARGV[2])
local now        = tonumber(ARGV[3])
local requested  = tonumber(ARGV[4])

local bucket = redis.call("HMGET", KEYS[1], "tokens", "ts")
local tokens = tonumber(bucket[1])
local ts     = tonumber(bucket[2])

if tokens == nil then
  tokens = capacity
  ts     = now
end

-- Refill for elapsed time, clamped to capacity.
local elapsed = math.max(0, now - ts)
tokens = math.min(capacity, tokens + elapsed * rate)

local allowed = 0
local retry_after = 0

if tokens >= requested then
  allowed = 1
  tokens  = tokens - requested
else
  retry_after = (requested - tokens) / rate
end

redis.call("HSET", KEYS[1], "tokens", tokens, "ts", now)
-- TTL: time for the bucket to refill completely, plus slack. Idle keys evaporate.
redis.call("EXPIRE", KEYS[1], math.ceil(capacity / rate) + 10)

return { allowed, math.floor(tokens), math.ceil(retry_after) }
```

### Why `now` is passed in rather than using `redis.call("TIME")`

Scripts that call `TIME` are non-deterministic, which historically made them
unreplicable to replicas and unsafe in some Redis configurations. Passing the
caller's clock keeps the script pure.

The trade-off is real and worth knowing: **clock skew between API replicas
distorts the refill.** A replica running 5 seconds fast grants 5 seconds of extra
refill. With NTP-synced hosts the skew is milliseconds and irrelevant. If you
ever can't trust the clocks, switch to `TIME` and accept the replication caveat.

### Loading the script

`SCRIPT LOAD` once at startup, then `EVALSHA`. `go-redis`'s `redis.NewScript`
does this automatically, falling back to `EVAL` on `NOSCRIPT` (which happens
after a Redis restart). Use it rather than managing SHAs yourself.

---

## 3. Layers

Every request passes through one or more limiters. **The first rejection wins**,
and they're ordered cheapest-first.

| Layer | Key | Purpose |
| --- | --- | --- |
| **Global per-IP** | `cf:v1:rl:ip:{ip}` | blunt DoS protection across all endpoints |
| **Per-route-class per-IP** | `cf:v1:rl:ip:{class}:{ip}` | tight limits on expensive or sensitive routes |
| **Per-user** | `cf:v1:rl:user:{class}:{user_id}` | stops one authenticated account abusing a route from many IPs |
| **Per-identifier** | `cf:v1:rl:email:{sha256(email)}` | login attempts *per account*, regardless of source IP |

The per-email layer is the one people skip, and it's the one that stops
distributed credential stuffing: 10,000 IPs each trying one password against one
account defeats a per-IP limit completely and is stopped cold by a per-email
counter.

Hash the email into the key — a Redis instance whose keyspace is a list of your
users' email addresses is an unnecessary disclosure if it's ever dumped.

### Configuration

| Route class | Per IP | Per user | Burst |
| --- | --- | --- | --- |
| `auth_login` | 5 / 15 min | — | 5 |
| `auth_register` | 3 / hour | — | 3 |
| `auth_refresh` | 30 / hour | 30 / hour | 10 |
| `public_read` | 120 / min | — | 120 |
| `auth_read` | 300 / min | 300 / min | 300 |
| `write` | 60 / min | 60 / min | 30 |
| `pledge` | 10 / min | 10 / min | 5 |
| `upload_presign` | 5 / hour | 20 / day | 5 |
| `download` | — | 3 / day | 3 |
| `webhook` | **exempt** | — | — |
| `health`, `metrics` | **exempt** | — | — |

Per-email: 5 failed logins / 15 min, and **only failures count**. Counting
successes locks out a legitimately busy user for no security benefit.

**Webhooks are exempt on purpose.** Rate-limiting Razorpay means dropping payment
notifications during exactly the traffic burst you most want to capture. The HMAC
signature is the gate. If you're worried about a flood of *invalid* signatures,
rate-limit those specifically after verification fails — never before.

---

## 4. Middleware

```go
func RateLimit(l *limiter.Limiter, class string, cfg Config) gin.HandlerFunc {
    return func(c *gin.Context) {
        keys := []limiter.Bucket{
            {Key: "ip:" + class + ":" + clientIP(c), Cap: cfg.IPBurst, Rate: cfg.IPRate},
        }
        if uid, ok := auth.UserIDFrom(c); ok {
            keys = append(keys, limiter.Bucket{
                Key: "user:" + class + ":" + uid.String(), Cap: cfg.UserBurst, Rate: cfg.UserRate})
        }

        res, err := l.AllowAll(c, keys...)
        if err != nil {
            metrics.RateLimitErrors.Inc()
            c.Next()                                    // FAIL OPEN — see §5
            return
        }

        c.Header("X-RateLimit-Limit", strconv.Itoa(res.Limit))
        c.Header("X-RateLimit-Remaining", strconv.Itoa(res.Remaining))
        c.Header("X-RateLimit-Reset", strconv.FormatInt(res.ResetUnix, 10))

        if !res.Allowed {
            c.Header("Retry-After", strconv.Itoa(res.RetryAfterSeconds))
            c.AbortWithStatusJSON(429, gin.H{"error": gin.H{
                "code": "RATE_LIMITED",
                "message": "Too many requests. Please retry shortly.",
                "request_id": requestid.From(c),
            }})
            return
        }
        c.Next()
    }
}
```

### `clientIP` behind a proxy

```go
func clientIP(c *gin.Context) string {
    // Gin's ClientIP respects TrustedProxies. SET THAT — the default trusts all
    // proxies, which means anyone can spoof X-Forwarded-For and bypass every
    // per-IP limit in the system.
    return c.ClientIP()
}
```

```go
router.SetTrustedProxies([]string{"10.0.0.0/8"})   // your LB's range only
```

This is not a small detail. With the default trust-everything setting, a single
attacker sends `X-Forwarded-For: <random>` on every request and gets an unlimited
number of distinct buckets. Your rate limiter then does nothing at all, silently.

For IPv6, bucket by **/64 prefix**, not the full address — a single client is
routinely handed a whole /64 and can otherwise rotate through 2^64 buckets.

---

## 5. Fail open

```go
if err != nil { c.Next(); return }     // Redis unreachable → allow
```

**The decision:** if Redis is down, requests are allowed.

Failing closed turns a Redis blip into a total outage of a site that was
otherwise perfectly healthy. Failing open means that during a Redis outage the
service is unprotected — but it is *serving*.

The mitigation that makes this defensible rather than lazy: a **per-process
in-memory fallback** with generous limits, active only while Redis is failing.

```go
if err != nil {
    metrics.RateLimitFallback.Inc()
    if !l.local.Allow(key) {              // 10× the Redis limit, per replica
        c.AbortWithStatus(429)
        return
    }
    c.Next()
    return
}
```

Degraded, not defenceless. Alert on `rate_limit_fallback_total > 0` — it means
Redis is unhealthy, which you want to know regardless.

Documented as [ADR-0006](DECISIONS/ADR-0006-rate-limiter-fail-open.md), because
"we chose to allow requests when the limiter is down" is a decision someone will
question and you want the reasoning on record rather than reconstructed.

---

## 6. Response headers

```
X-RateLimit-Limit: 120
X-RateLimit-Remaining: 87
X-RateLimit-Reset: 1785412800
Retry-After: 12                 (429 only)
```

Report the **most restrictive** bucket's numbers when several apply — otherwise a
client sees "87 remaining" from the global bucket and gets a 429 from the
per-route one, which looks like a bug in your API.

---

## 7. Metrics

```
rate_limit_decisions_total{class, layer, result="allowed|limited"}
rate_limit_errors_total
rate_limit_fallback_total
rate_limit_check_duration_seconds{class}
```

`rate_limit_decisions_total{result="limited"}` rising sharply on `auth_login` is
a credential-stuffing signal, and it's worth an alert rather than just a
dashboard.

---

## 8. Tuning

Start permissive and tighten with data. A limit that blocks legitimate users
generates support load and teaches you nothing; a limit that's slightly loose
still stops the automated abuse it's aimed at.

Method: run for a week, look at the p99 request rate per IP and per user per
route class, set the limit at roughly **3× p99**. Recheck monthly.

Two exceptions where you should be strict from day one, because the downside is
asymmetric: `auth_login` (credential stuffing) and `upload_presign` (a presign is
cheap for you to issue and expensive in the storage it authorises).

---

## 9. Tests

| # | Scenario | Assertion |
| --- | --- | --- |
| R1 | 6 logins in 15 min from one IP | 6th returns 429 with `Retry-After` |
| R2 | Wait for refill | request succeeds again |
| R3 | Two IPs | independent buckets |
| R4 | Same user, two IPs, per-user limit | the *user* limit still applies |
| R5 | 100 concurrent requests, limit 10 | exactly 10 allowed (Lua atomicity proof) |
| R6 | Redis down | requests allowed, `rate_limit_fallback_total` increments |
| R7 | Spoofed `X-Forwarded-For` from an untrusted source | ignored; the real IP is bucketed |
| R8 | Webhook endpoint under load | never limited |
| R9 | Failed logins for one email from 50 IPs | per-email limit trips |
| R10 | Two IPv6 addresses in the same /64 | share a bucket |

**R5 is the test that proves the Lua script**, and it's the one that fails if you
ever refactor the atomic script into separate Go-side calls. Write it early and
let it stand guard.
