# ADR-0007: Reward tiers claimed at capture, with soft holds

**Status:** accepted
**Date:** 2026-08-02

## Context

A reward tier may have a `quantity_limit` — "Executive Producer credit, 10
available". Rule F4 says a limited tier must never be claimed more times than its
limit.

The complication is that a pledge is not instantaneous. Between "backer clicks
pledge" and "payment captured" there is a Razorpay Checkout session that may take
seconds or minutes, may be abandoned, and may fail. So there is a window during
which a slot is neither free nor taken.

## Options considered

**Increment `claimed_count` at pledge creation.** The slot is reserved the moment
checkout begins, so nobody who reaches payment can be told "sold out". But every
abandoned checkout permanently consumes a slot, and a script can exhaust a
limited tier for free by creating pledges it never pays for. For a 10-slot tier
that is trivial to do.

**Increment at capture.** No slot is consumed until money actually arrives, and
abandonment costs nothing. But two backers can both start checkout for the last
slot; one pays and gets it, the other pays and is told it's gone — after being
charged. Refunding them is possible but it is a genuinely bad experience and a
support burden.

**Soft holds with expiry.** Creating a pledge inserts a `tier_holds` row with a
15-minute TTL. Availability is computed as `claimed_count + active_holds`. A
capture converts the hold into a claim; expiry or failure releases it.

## Decision

**Increment `claimed_count` at capture, with soft holds on top.**

- `POST /pledges` takes a `FOR UPDATE` lock on the tier, checks
  `claimed_count + active_holds < quantity_limit`, and inserts a hold expiring in
  15 minutes.
- Capture converts the hold: `claimed_count += 1`, hold deleted, inside the same
  transaction as the pledge state change.
- Holds are swept by the scheduler; expiry is also enforced by the availability
  query so a lagging sweep never over-reserves.

**The database is the final guard**, not the application check:

```sql
CONSTRAINT chk_tier_not_oversold
    CHECK (quantity_limit IS NULL OR claimed_count <= quantity_limit)
```

The `FOR UPDATE` read exists so the common case returns a clean
`409 TIER_SOLD_OUT` instead of a constraint violation. The constraint exists
because under concurrency the application check races and the constraint cannot.

**Acceptable v1 simplification:** ship capture-time counting only, without holds,
*provided* the UI never advertises an exact remaining count below ~5. That avoids
"1 left!" followed by a post-payment failure. Holds can be added later without
schema changes to `pledges`. Prefer shipping holds if the schedule allows —
retrofitting them means reasoning about pledges that are already in flight.

## Consequences

**Good**

- Abandoned checkouts cost nothing after 15 minutes.
- A backer who reaches payment almost always gets the slot.
- Free exhaustion of a tier is bounded to 15 minutes and rate-limited by the
  `pledge` route class (10/min per user).
- Oversell is structurally impossible, not merely unlikely.

**Bad**

- A third table and a sweep job to maintain.
- The 15-minute window still allows temporary exhaustion by a determined script.
  Mitigated by rate limiting, not eliminated.
- The availability number shown to users is `limit − claimed − holds`, which can
  briefly under-report. Displaying "few remaining" rather than an exact count
  below a threshold is the honest presentation.
- A capture arriving after its hold expired must still succeed if a slot is free,
  and must fail cleanly with a refund if not. That path needs an explicit test.

**Commits us to**

- The hold TTL exceeding the Razorpay order expiry, or holds release before
  payments can complete.
- Testing scenario P7 (two concurrent pledges for the last slot) as a
  first-class concurrency test, not an afterthought.
