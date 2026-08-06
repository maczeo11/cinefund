# ADR-0003: Two-layer webhook idempotency

**Status:** accepted
**Date:** 2026-08-02

## Context

Razorpay delivers webhooks asynchronously and **retries on any non-2xx response
or timeout**. The same `payment.captured` event can therefore arrive several
times, possibly concurrently, possibly out of order relative to other events for
the same payment.

The naive handler does `campaign.raised += payload.amount`. Two deliveries
double-credit the campaign, the ledger no longer reflects reality, and an
all-or-nothing campaign can be declared funded when it isn't. This is the single
highest-impact bug the system can have, and it is silent.

## Options considered

**Redis `SETNX` only.** Fast — a duplicate never touches the database. But Redis
is not durable in the way this needs: keys can be evicted under memory pressure,
lost on failover, or removed by an operator running `FLUSHDB`. Any of those turns
a retry into a double-credit. It is a cache being used as a correctness
guarantee.

**Postgres unique constraint only.** Durable and transactional. A duplicate hits
`23505` and the whole transaction rolls back atomically. But every retry — and a
provider retry storm during an outage can be substantial — reaches the database
and consumes a connection.

**Application-level "have I seen this?" check before the transaction.** A
`SELECT` followed by an `INSERT` is a race. Two concurrent deliveries both see
"not seen", both proceed. This is the option that looks correct and isn't.

**Both Redis and Postgres.**

## Decision

Two layers, with clearly different jobs.

**Layer 1 — Redis `SETNX idem:wh:{event_id}` with a 24-hour TTL.** A performance
optimisation. Absorbs retry storms without touching Postgres. If Redis is
unavailable, log a warning and continue — it is not required for correctness.

**Layer 2 — `UNIQUE (provider, provider_event_id)` on `payment_events`,
inserted as the first statement of the processing transaction.** This is the
guarantee. A `23505` means "already processed": return `ErrDuplicateEvent`, roll
back, respond 200.

**Critically: if the transaction fails for any reason other than duplicate,
delete the Redis key.** Otherwise the sequence is — transaction fails → we return
500 → provider retries → Redis says duplicate → we return 200 → **the event is
lost forever.** That single missing `DEL` is a money-losing bug that only appears
during an incident.

## Consequences

**Good**

- Correct under Redis loss, Redis eviction, concurrent delivery, and process
  crashes mid-transaction.
- The common duplicate costs one Redis round trip, not a database transaction.
- `payment_events` doubles as a permanent audit trail — the exact bytes the
  provider sent, which is the only thing that settles a dispute months later.

**Bad**

- Two mechanisms to understand and to keep in sync. The `DEL`-on-failure rule is
  subtle and easy to drop during a refactor; scenario P5 exists to catch that.
- `payment_events` grows without bound. Needs a retention policy (keep 2 years —
  it's small, and it's financial).
- Redis TTL (24 h) is shorter than the provider's maximum retry window in
  pathological cases. Acceptable, because layer 2 covers it.

**Commits us to**

- Returning **5xx on any failure to record**, never 200. Acknowledging an event
  you dropped is worse than being retried.
- Consumers of the resulting Kafka events being idempotent too, since the outbox
  is at-least-once.

## Test that proves it

Scenario P2: fire the same signed webhook 50 times concurrently, released from a
single `close(start)` channel. Assert exactly one `payment_events` row, one
`ledger_transactions` row, and `raised_amount` incremented exactly once — with
all 50 responses being 200. Then P5: flush Redis between two deliveries and
assert the same outcome.
