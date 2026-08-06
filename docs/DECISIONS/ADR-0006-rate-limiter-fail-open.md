# ADR-0006: The rate limiter fails open

**Status:** accepted
**Date:** 2026-08-02

## Context

Rate limiting uses a Redis-backed token bucket so limits are shared across API
replicas. That puts Redis on the critical path of **every** request.

When Redis is unreachable, the middleware must decide: allow the request, or
reject it?

This is a security-versus-availability trade with no free answer, which is
exactly why it deserves a record rather than an accident.

## Options considered

**Fail closed — reject when the limiter is unavailable.** Maximally safe: limits
are never bypassed. But it converts a Redis blip into a **total outage** of a
service that was otherwise perfectly healthy. Postgres is fine, the API is fine,
and yet every user gets a 503 because a cache-tier dependency is unavailable. For
a system whose most important job is accepting payments, this is a bad trade.

**Fail open — allow when the limiter is unavailable.** The service keeps serving.
But during the outage there is no protection against credential stuffing or
scraping, and the window is unbounded.

**Fail open, with an in-process fallback bucket.** Keep serving, but not
defenceless.

## Decision

**Fail open**, with two mitigations that make it a considered choice rather than
a shrug:

1. **A per-process in-memory token bucket** with roughly 10× the Redis limit,
   used only while Redis is failing. With a handful of replicas the effective
   limit is loose but finite — enough to stop an automated attack from running at
   full speed.
2. **`rate_limit_fallback_total` is a ticket-level alert.** Any increase means
   Redis is unhealthy, which needs attention regardless of the rate-limiting
   consequences.

Redis calls in the limiter get a **50 ms timeout**. A hung Redis must not become
a hung request; the timeout expires, the fallback engages, and the request
proceeds.

**Two things do not fail open, and the contrast is the point:**

- **Webhook processing fails closed.** If the event cannot be recorded, return
  5xx so the provider retries. Returning 200 for a dropped event loses money
  silently.
- **Entitlement checks fail closed.** If Postgres cannot confirm an entitlement,
  deny playback. Availability is not worth serving backer-only content to
  everyone.

The rule that generalises: **fail open on rate limiting and caching; fail closed
on authorisation and money.**

## Consequences

**Good**

- A Redis outage degrades one feature instead of taking the site down.
- The fallback bucket means "degraded", not "defenceless".
- The 50 ms timeout bounds the latency impact of a slow Redis.
- The alert means the underlying problem is visible even though it was absorbed.

**Bad**

- During a Redis outage the effective limit is `10 × configured × replica_count`,
  which on the login endpoint is a real, if bounded, exposure window.
- The fallback is per-process, so it provides no protection against an attacker
  spreading requests across replicas.
- An attacker who could induce a Redis outage could deliberately weaken rate
  limiting. Accepted: an attacker with that capability has larger options
  available.

**Commits us to**

- Login also being protected by non-rate-limit controls that don't depend on
  Redis: Argon2id cost, generic error messages, and constant-time responses for
  unknown accounts. Rate limiting is a layer, not the whole defence.
- Monitoring Redis health as a first-class concern rather than treating it as
  optional infrastructure.

## Reconsider if

Authentication abuse becomes a real, observed problem rather than a theoretical
one. At that point the login endpoint specifically could fail closed while
everything else continues to fail open — a per-route policy rather than a global
one.
