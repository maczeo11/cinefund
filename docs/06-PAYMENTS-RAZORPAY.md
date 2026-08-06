# 06 — Payments (Razorpay)

The most correctness-sensitive part of the system. Everything here exists to
answer one question: **after any sequence of network failures, retries, and
process crashes, is the money right?**

---

## 1. Integration model

CineFund uses **Razorpay Orders + Checkout + Webhooks**. Card data never touches
this codebase — the Checkout widget collects it and posts directly to Razorpay.
That keeps the whole system out of PCI-DSS scope beyond SAQ-A.

```
pledge (our id)  ──1:1──►  Razorpay Order  ──1:N──►  Razorpay Payment
                                                          │
                                                          └─1:N──► Refund
```

An order can have several payment attempts (user's card declines, retries with
another). **Only one can succeed.** `pledges.provider_payment_id UNIQUE`
enforces that at our end.

### Amounts

Razorpay uses paise for INR — the same unit as our `BIGINT`. No conversion, no
rounding, no float. If you ever find yourself writing `amount * 100`, stop and
check which side is already in paise.

### Config

| Env | Purpose |
| --- | --- |
| `RAZORPAY_KEY_ID` | publishable; sent to the browser |
| `RAZORPAY_KEY_SECRET` | server-only; Basic auth for the REST API |
| `RAZORPAY_WEBHOOK_SECRET` | server-only; HMAC key for webhook verification. **Different from the key secret.** |

The webhook secret being a separate value is the thing everyone misses on the
first attempt, and the symptom is 100% signature failures.

---

## 2. Order creation

`POST /api/v1/campaigns/{id}/pledges` — see [04 §6](04-API-SPEC.md#6-pledges) for
the validation sequence.

```go
func (s *Service) CreatePledge(ctx context.Context, actor Actor, in Input) (*Pledge, error) {
    // 1. Validate + reserve the tier, commit the pledge row. Short transaction.
    var pledge *Pledge
    err := s.tx.Do(ctx, func(q Queries) error {
        c, err := q.GetCampaignForUpdate(ctx, in.CampaignID)
        if err != nil { return err }
        if c.Status != StatusLive              { return errs.Conflict("CAMPAIGN_NOT_LIVE") }
        if time.Now().After(c.Deadline.Add(-60*time.Second)) { return errs.Conflict("DEADLINE_PASSED") }
        if c.CreatorID == actor.UserID         { return errs.Forbidden("CREATOR_CANNOT_PLEDGE") }

        tier, err := q.GetTierForUpdate(ctx, in.TierID)     // row lock
        if err != nil { return err }
        if tier.CampaignID != c.ID             { return errs.Invalid("tier does not belong to campaign") }
        if in.Amount < tier.MinAmount          { return errs.Invalid("AMOUNT_BELOW_TIER") }
        if tier.SoldOut()                      { return errs.Conflict("TIER_SOLD_OUT") }

        pledge, err = q.InsertPledge(ctx, ...)              // status CREATED
        return err
    })
    if err != nil { return nil, err }

    // 2. External call — OUTSIDE the transaction. Never hold a row lock across the network.
    order, err := s.gateway.CreateOrder(ctx, razorpay.OrderRequest{
        Amount:         pledge.Amount,
        Currency:       "INR",
        Receipt:        pledge.ID.String(),      // our id travels with the order
        Notes:          map[string]string{"pledge_id": pledge.ID.String(),
                                          "campaign_id": in.CampaignID.String()},
        PaymentCapture: 1,                        // auto-capture
    })
    if err != nil {
        // The pledge stays CREATED with no order id. The reconciliation sweep
        // will fail it after 15 minutes. Do NOT delete it — a deleted row is a
        // lost audit trail, and the order might have been created after all.
        return nil, errs.Unavailable("PAYMENT_PROVIDER_UNAVAILABLE")
    }

    if err := s.repo.AttachOrder(ctx, pledge.ID, order.ID); err != nil {
        // Order exists at Razorpay but we didn't record it. The webhook's
        // `receipt` field still carries our pledge id, so we can recover.
        s.log.Error("order created but not attached", "pledge_id", pledge.ID, "order_id", order.ID)
    }
    return pledge, nil
}
```

Three deliberate decisions in that function, each of which is a question you
should be able to answer:

1. **Why is `CreateOrder` outside the transaction?** Because a Postgres
   transaction holding `FOR UPDATE` on a popular tier row while waiting 300 ms
   for an external HTTP call serialises every checkout on that campaign. Under
   any real load, that's the bottleneck.
2. **Why keep the pledge row when the order fails?** It's the audit trail, and
   Razorpay might have created the order and lost the response. `receipt =
   pledge_id` means a webhook can still find its way home.
3. **Why does `Receipt` carry our id?** So that if `provider_order_id` was never
   written, the webhook can look up the pledge by `receipt` instead. Two paths
   to the same row is cheap insurance.

### Tier reservation: the honest trade-off

The code above increments `claimed_count` at **capture**, not at pledge creation.
That means a limited tier can be "reserved" by more people than can actually get
it, and the losers get a `TIER_SOLD_OUT` error after paying — bad.

The alternative — increment at creation — means abandoned checkouts hold slots
until they expire, and a bot can exhaust a tier for free.

**Decision: increment at capture, plus a soft reservation.** On pledge creation,
`INSERT` into a `tier_holds` row with a 15-minute expiry, and count
`claimed + active_holds` against the limit. Holds are released by expiry or by
capture converting them. This is what event ticketing does and it's the right
shape. If you want to ship faster, capture-time only is acceptable for v1 as
long as you never advertise a tier as having exactly 1 remaining. Recorded in
[ADR-0007](DECISIONS/ADR-0007-tier-reservation.md).

---

## 3. The webhook handler

The single most important function in the codebase.

### Reading the raw body

```go
// internal/platform/httpx/rawbody.go
func CaptureRawBody(maxBytes int64) gin.HandlerFunc {
    return func(c *gin.Context) {
        body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBytes))
        if err != nil { c.AbortWithStatus(http.StatusBadRequest); return }
        c.Set("raw_body", body)
        c.Request.Body = io.NopCloser(bytes.NewReader(body))   // restore for binding
        c.Next()
    }
}
```

Register it **only** on the webhook route. Buffering every request body globally
is wasteful, and buffering a 4 GB upload would be catastrophic — though uploads
never hit the API, this is exactly the kind of "only on this route" discipline
that prevents a future accident.

### The handler

```go
func RazorpayWebhook(s *pledge.Service) gin.HandlerFunc {
    return func(c *gin.Context) {
        raw := c.MustGet("raw_body").([]byte)
        sig := c.GetHeader("X-Razorpay-Signature")

        if err := crypto.VerifyRazorpaySignature(raw, sig, cfg.WebhookSecret); err != nil {
            s.RecordInvalidSignature(c, raw)          // for the alert
            c.JSON(401, gin.H{"error": "invalid signature"})
            return
        }

        var evt razorpay.WebhookEvent
        if err := json.Unmarshal(raw, &evt); err != nil {
            c.JSON(400, gin.H{"error": "malformed"})   // 400 = don't retry, it'll never parse
            return
        }

        switch err := s.HandleWebhook(c, evt, raw); {
        case err == nil:
            c.JSON(200, gin.H{"status": "ok"})
        case errors.Is(err, pledge.ErrDuplicateEvent):
            c.JSON(200, gin.H{"status": "duplicate"})  // 200 stops the retries
        default:
            s.log.Error("webhook processing failed", "event_id", evt.ID, "error", err)
            c.JSON(500, gin.H{"error": "processing failed"})   // 5xx = please retry
        }
    }
}
```

**The status-code contract, which is the whole game:**

| Outcome | Status | Provider behaviour | Why |
| --- | --- | --- | --- |
| Processed | 200 | stops | done |
| Duplicate | 200 | stops | already done; retrying achieves nothing |
| Bad signature | 401 | stops | it isn't from Razorpay; retrying won't help |
| Unparseable | 400 | stops | it will never parse |
| **DB down / any failure to record** | **500** | **retries with backoff** | we did *not* record it; we need it again |
| Unknown event type | 200 | stops | recorded in `payment_events`, nothing else to do |

Returning 200 for something you failed to process is how money silently
disappears. When in doubt, 5xx.

### Idempotency: two layers

```go
func (s *Service) HandleWebhook(ctx context.Context, evt WebhookEvent, raw []byte) error {
    // ---- Layer 1: Redis fast path. Optional. Stops retry storms cheaply.
    lockKey := "idem:wh:" + evt.ID
    acquired, err := s.redis.SetNX(ctx, lockKey, "1", 24*time.Hour).Result()
    if err != nil {
        s.log.Warn("redis unavailable for idempotency; relying on postgres", "error", err)
    } else if !acquired {
        return ErrDuplicateEvent
    }

    // ---- Layer 2: Postgres. Mandatory. The actual guarantee.
    err = s.tx.Do(ctx, func(q Queries) error {
        if err := q.InsertPaymentEvent(ctx, evt); err != nil {
            if pgerr.IsUnique(err) { return ErrDuplicateEvent }   // uq_provider_event
            return err
        }
        switch evt.Event {
        case "payment.captured":  return s.applyCapture(ctx, q, evt)
        case "payment.failed":    return s.applyFailure(ctx, q, evt)
        case "refund.processed":  return s.applyRefund(ctx, q, evt)
        case "refund.failed":     return s.applyRefundFailure(ctx, q, evt)
        default:                  return nil          // recorded, unhandled
        }
    })

    // If we failed for any reason other than duplicate, release the Redis key so
    // the provider's retry isn't swallowed by our own fast path.
    if err != nil && !errors.Is(err, ErrDuplicateEvent) {
        s.redis.Del(ctx, lockKey)
    }
    return err
}
```

### Why both layers

This is the question an interviewer will ask, so the answer needs to be crisp:

> Redis `SETNX` is a **performance** optimisation. It absorbs a retry storm
> without touching Postgres. It is **not** a correctness guarantee, because Redis
> can evict the key under memory pressure, lose it on failover, or be flushed.
>
> The unique constraint on `payment_events(provider, provider_event_id)` is the
> **correctness** guarantee. It is durable, it participates in the transaction,
> and if it fires the entire state change rolls back atomically.
>
> If I could only have one, I'd keep the constraint.

The `s.redis.Del` on failure is the subtle bit. Without it: transaction fails
→ we return 500 → Razorpay retries → Redis says "duplicate" → we return 200 →
**the event is lost forever**. That single missing line is a money-losing bug,
and it's the kind that only shows up during an incident.

---

## 4. Applying a capture

```go
func (s *Service) applyCapture(ctx context.Context, q Queries, evt WebhookEvent) error {
    p := evt.Payload.Payment.Entity

    pledge, err := q.GetPledgeByOrderIDForUpdate(ctx, p.OrderID)
    if errors.Is(err, sql.ErrNoRows) {
        pledge, err = q.GetPledgeForUpdate(ctx, uuid.MustParse(p.Notes["pledge_id"]))  // fallback
    }
    if err != nil { return err }

    if pledge.Status == StatusCaptured || pledge.Status == StatusSettled {
        return nil            // already applied by a prior delivery; not an error
    }
    if !pledge.Status.CanTransitionTo(StatusCaptured) {
        return fmt.Errorf("illegal transition %s → CAPTURED for pledge %s", pledge.Status, pledge.ID)
    }
    if p.Amount != pledge.Amount {
        return fmt.Errorf("amount mismatch: paid %d, pledged %d", p.Amount, pledge.Amount)
    }

    if err := q.MarkPledgeCaptured(ctx, pledge.ID, p.ID, time.Now()); err != nil { return err }
    if err := q.IncrementCampaignRaised(ctx, pledge.CampaignID, pledge.Amount); err != nil { return err }
    if pledge.TierID != nil {
        if err := q.IncrementTierClaimed(ctx, *pledge.TierID); err != nil { return err }  // chk_tier_not_oversold guards
    }
    if err := s.ledger.RecordPledgeCapture(ctx, q, pledge, p.Fee, p.Tax); err != nil { return err }

    return q.InsertOutbox(ctx, Event{
        Type: "pledge.captured", AggregateType: "pledge", AggregateID: pledge.ID,
        Payload: PledgeCapturedV1{PledgeID: pledge.ID, CampaignID: pledge.CampaignID,
                                  BackerID: pledge.BackerID, Amount: pledge.Amount},
        TraceID: telemetry.TraceIDFrom(ctx),
    })
}
```

**Everything in one transaction:** pledge status, campaign counter, tier counter,
ledger entries, outbox row. Either all of it happened or none of it did. That
atomicity is the entire reason Postgres was chosen for this data.

**The amount check is not paranoia.** If the amounts disagree, something is
genuinely wrong (a tampered order, a mismatched environment, a bug) and you want
a 500 and an alert, not a silent credit of the wrong number.

**The `already captured → return nil` branch** handles the legitimate case of
Razorpay delivering the same event via a different `event_id` (it happens on
their retries after a partial outage). Idempotency has to be checked at the
*state* level too, not only the *event* level.

---

## 5. Refunds

Two triggers: campaign failed/cancelled (system), or backer cancelled (rule F9).

```go
func (s *Service) InitiateRefund(ctx context.Context, pledgeID uuid.UUID, reason RefundReason) error {
    var refund *Refund
    err := s.tx.Do(ctx, func(q Queries) error {
        p, err := q.GetPledgeForUpdate(ctx, pledgeID)
        if err != nil { return err }
        if p.Status != StatusCaptured { return errs.Conflict("pledge is not refundable in state %s", p.Status) }

        refund, err = q.InsertRefund(ctx, Refund{
            PledgeID: p.ID, Amount: p.Amount, Reason: reason, Status: RefundPending,
            // Deterministic key: retrying this whole flow produces the same key,
            // and Razorpay dedupes on it.
            IdempotencyKey: "refund:" + p.ID.String(),
        })
        if pgerr.IsUnique(err) { return ErrRefundAlreadyExists }   // uq_refund_active_per_pledge
        if err != nil { return err }

        if err := q.SetPledgeStatus(ctx, p.ID, StatusRefundPending); err != nil { return err }
        return q.InsertOutbox(ctx, Event{Type: "refund.requested", AggregateID: p.ID, ...})
    })
    if err != nil { return err }

    // Call Razorpay outside the transaction, from the consumer of refund.requested.
    return nil
}
```

The actual API call happens in a **Kafka consumer**, not inline. Reason: a failed
campaign with 200 backers means 200 refund API calls. Doing them inside the
deadline-sweep transaction would hold a transaction open for minutes. Emitting
200 `refund.requested` events and letting a consumer process them with retries
and backoff is the correct shape.

The refund lifecycle:

```
refund.requested (outbox → Kafka)
    → consumer calls Razorpay Refunds.Create with the deterministic idempotency key
    → refund.status = PROCESSING, provider_refund_id stored
    → ... asynchronously ...
    → webhook refund.processed → status COMPLETED, pledge REFUNDED, ledger reversal
    → or webhook refund.failed  → status FAILED, alert, manual queue
```

`REFUND_FAILED` is a state that needs a human. Surface it in the admin panel with
a loud counter. A silently failed refund is a complaint and possibly a chargeback.

---

## 6. Idempotency keys on client requests

Distinct from webhook idempotency. This covers "the client retried because the
response timed out".

```
Client                          API
  │  POST /pledges                │
  │  Idempotency-Key: 7f3a-…      │
  │──────────────────────────────►│  INSERT idempotency_keys (IN_FLIGHT)
  │                               │    ├ unique violation + status=COMPLETED → replay stored response
  │                               │    ├ unique violation + status=IN_FLIGHT → 409 REQUEST_IN_PROGRESS
  │      (timeout, no response)   │    └ inserted → proceed
  │◄─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─│  ... handler runs, UPDATE → COMPLETED + response_body
  │  POST /pledges (same key)     │
  │──────────────────────────────►│  → replays the stored 201. No second Razorpay order.
```

Rules:

- Key is scoped `(user_id, endpoint, key)`. Never global — see
  [02 §7](02-DATA-MODEL-POSTGRES.md#7-idempotency-keys-client-supplied).
- `request_hash` (SHA-256 of the canonical body) must match. Same key with a
  different body is a client bug → `422 IDEMPOTENCY_KEY_REUSED`.
- Only successful responses (2xx) are stored and replayed. A 500 should be
  retryable, so don't cache it.
- TTL 24 hours, swept by the scheduler.

Required on: `POST /pledges`, `POST /media/uploads`, `POST /campaigns/{id}/payout`.
Recommended everywhere a POST creates something.

---

## 7. Reconciliation

Everything above assumes webhooks arrive. Sometimes they don't — a misconfigured
URL, a deploy during a delivery window, a provider incident. Reconciliation is
what makes the system self-healing instead of quietly wrong.

Runs in `cmd/scheduler` every 15 minutes:

```
A. Stale pledges
   SELECT id, provider_order_id FROM pledges
    WHERE status = 'CREATED' AND created_at < now() - interval '15 minutes'
   For each:
     - no order id           → mark FAILED (reason: order_creation_failed)
     - has order id          → GET /orders/{id}/payments from Razorpay
         · a captured payment exists → apply it exactly as the webhook would,
           through the SAME code path (this is why applyCapture takes an event,
           not an HTTP request — you can synthesise one)
         · none, order expired       → mark FAILED
         · none, order still open    → leave it, try next cycle

B. Stuck refunds
   refunds in PROCESSING for > 2 hours → poll Razorpay, apply or alert

C. Invariant checks  (02 §9)
   I1  every ledger transaction balances
   I2  campaigns.raised_amount == SUM(captured pledges)
   I3  no tier oversold
   I6  no negative escrow
   I7  no LIVE campaign past deadline by > 5 min
   Any failure → ERROR log + Prometheus counter + admin dashboard row.
   Do NOT auto-correct. A silent auto-fix hides the bug that caused the drift.
```

Point A is why `applyCapture` is written to take a webhook event struct rather
than a `*gin.Context`. The reconciler builds the same struct from a polled API
response and calls the identical function. **One code path for applying a
capture, two ways to reach it.** Two implementations of "apply a capture" would
inevitably diverge, and the divergence would be in the rarely-exercised one.

---

## 8. Test scenarios

Each of these is an integration test against testcontainers. They are the
tests that matter most in the entire project.

| # | Scenario | Expected |
| --- | --- | --- |
| P1 | Happy path: pledge → order → capture webhook | pledge `CAPTURED`, `raised += amount`, 2+ balanced ledger entries, 1 outbox row |
| P2 | Same webhook delivered 50× concurrently | exactly one state change; 49 return 200 `duplicate` |
| P3 | Webhook with a tampered body | 401, no state change, `signature_valid=false` recorded |
| P4 | Webhook arrives while Postgres is down | 500 returned; after recovery a retry applies it exactly once |
| P5 | Redis flushed between two deliveries of the same event | Postgres constraint catches it; still exactly one state change |
| P6 | Capture whose amount ≠ pledge amount | 500, alert, no state change |
| P7 | Two concurrent pledges to the last slot of a limited tier | one succeeds, one gets 409 `TIER_SOLD_OUT`; `claimed_count == limit` |
| P8 | Campaign fails at deadline with 3 captured pledges | 3 refunds `PENDING`, 3 `refund.requested` events, escrow drains to 0 after processing |
| P9 | Refund webhook delivered twice | one `REFUNDED`, one reversal transaction |
| P10 | Client retries `POST /pledges` with the same Idempotency-Key | one pledge, one Razorpay order, identical 201 replayed |
| P11 | Same Idempotency-Key, different body | 422 |
| P12 | Order created but `AttachOrder` fails, then webhook arrives | pledge found via `notes.pledge_id`, capture applied |
| P13 | Webhook never arrives; reconciliation runs | polled from Razorpay and applied identically |
| P14 | Pledge submitted 30 s before the deadline | 409 `DEADLINE_PASSED` (the 60 s margin) |

P2, P4, P5 and P12 are the ones worth demoing in a recording. They're the ones
that look like magic and are actually just careful constraint design.
