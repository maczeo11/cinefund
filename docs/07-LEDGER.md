# 07 — The Ledger

Why an integer column isn't enough, and what to build instead.

---

## 1. Why not just `campaigns.raised_amount += x`

That column exists and is useful — it's what the campaign page displays. But it
cannot answer any of these:

- How much of the ₹6,12,500 raised is still refundable?
- What did the platform earn on this campaign, and when was it earned?
- A backer says they were charged twice. Prove it either way.
- The gateway's settlement report says ₹4,81,220. Ours says ₹4,81,900. Which
  line item differs?
- After 200 refunds, is campaign escrow exactly zero?

A single mutable counter has no history, no attribution, and no way to detect
drift. **Double-entry gives you an audit trail that is checkable by arithmetic.**
Every rupee that moves is recorded twice — where it came from and where it went —
and the sum of all entries in a transaction is always zero. If it isn't, you have
a bug, and you find out immediately rather than during a dispute.

---

## 2. Accounts

Six account kinds. `owner_id` scopes the per-entity ones.

| Kind | Owner | Normal balance | Meaning |
| --- | --- | --- | --- |
| `PLATFORM_ESCROW` | none | debit | total funds the platform is holding, across all campaigns |
| `CAMPAIGN_ESCROW` | campaign_id | credit | this campaign's share of that money |
| `BACKER_REFUND_PAYABLE` | user_id | credit | money owed back to a backer, not yet sent |
| `CREATOR_PAYABLE` | user_id | credit | money owed to a creator after a successful campaign |
| `PLATFORM_FEE_REVENUE` | none | credit | our 7%, recognised at payout |
| `GATEWAY_FEE_EXPENSE` | none | debit | what Razorpay charged us |

### Debits and credits, without the accounting course

You only need one rule, and it's mechanical:

> **DEBIT** increases assets (money you hold) and expenses.
> **CREDIT** increases liabilities (money you owe) and revenue.
> Every transaction's debits must equal its credits.

Escrow is an asset — we hold it — so receiving money **debits** escrow. It is
simultaneously a liability, because we owe it to either the creator or the
backers depending on the outcome, so it **credits** the campaign's escrow
account. Those two together are the "money arrived and it belongs to campaign X"
statement.

If the direction of an entry ever feels ambiguous, ask: *did the platform's
holdings go up or down?* Up is a debit to escrow.

---

## 3. Every money movement

These five transaction kinds are the complete set. Anything that isn't one of
these is an `ADJUSTMENT` and requires an admin and a memo.

### 3.1 `PLEDGE_CAPTURE` — a backer's payment succeeds

Backer pledges ₹1,000 (100000 paise). Razorpay's fee is ₹23.60 (2360 paise,
including GST), deducted from settlement, not from the backer.

| Account | Direction | Amount |
| --- | --- | --- |
| `PLATFORM_ESCROW` | DEBIT | 100000 |
| `CAMPAIGN_ESCROW(campaign)` | CREDIT | 100000 |
| `GATEWAY_FEE_EXPENSE` | DEBIT | 2360 |
| `PLATFORM_ESCROW` | CREDIT | 2360 |

Sum: `(100000 + 2360) − (100000 + 2360) = 0`. ✅

Reading it in English: we received ₹1,000 and it belongs to this campaign; and
₹23.60 of what we hold has already been consumed by the gateway.

Note the gateway fee reduces **platform escrow**, not campaign escrow. The
campaign is owed its full ₹1,000; the fee is the platform's cost of doing
business, recovered later from the 7%. Choosing the other model (backer's
pledge nets down) would mean a campaign that raises exactly its goal comes up
short, which is a bad product experience and a support nightmare.

### 3.2 `REFUND` — a pledge is returned

| Account | Direction | Amount |
| --- | --- | --- |
| `CAMPAIGN_ESCROW(campaign)` | DEBIT | 100000 |
| `BACKER_REFUND_PAYABLE(backer)` | CREDIT | 100000 |

...recorded when the refund is **initiated**. Then, when `refund.processed`
arrives and the money has actually left:

| Account | Direction | Amount |
| --- | --- | --- |
| `BACKER_REFUND_PAYABLE(backer)` | DEBIT | 100000 |
| `PLATFORM_ESCROW` | CREDIT | 100000 |

Two transactions, not one, because there is a real interval during which the
money is *owed but not yet sent*. Collapsing them means your books claim the
money left before it did, and a refund that fails at the gateway leaves you with
no record of the obligation.

The gateway fee on the original capture is **not** returned by Razorpay. That
loss stays in `GATEWAY_FEE_EXPENSE` — which is correct, and is exactly why
platforms don't love failed campaigns.

### 3.3 `FEE` + `PAYOUT` — a successful campaign settles

Campaign raised ₹6,12,500 (61250000 paise). Platform fee 7% = 4287500. Net to
creator = 56962500.

**Fee recognition:**

| Account | Direction | Amount |
| --- | --- | --- |
| `CAMPAIGN_ESCROW(campaign)` | DEBIT | 4287500 |
| `PLATFORM_FEE_REVENUE` | CREDIT | 4287500 |

**Creator obligation:**

| Account | Direction | Amount |
| --- | --- | --- |
| `CAMPAIGN_ESCROW(campaign)` | DEBIT | 56962500 |
| `CREATOR_PAYABLE(creator)` | CREDIT | 56962500 |

After these two, `CAMPAIGN_ESCROW` for this campaign is **exactly zero**. That's
the invariant to assert in the test: a fully settled campaign has an empty escrow
account. If it doesn't, either a pledge was missed or the fee arithmetic drifted.

**Payment sent:**

| Account | Direction | Amount |
| --- | --- | --- |
| `CREATOR_PAYABLE(creator)` | DEBIT | 56962500 |
| `PLATFORM_ESCROW` | CREDIT | 56962500 |

### 3.4 Rounding

`fee = raised * 7 / 100` in integer arithmetic, truncating. `net = raised - fee`,
computed by subtraction — never by `raised * 93 / 100`, which can differ from
`raised - fee` by one paisa and produce an unbalanced transaction.

`chk_payout_math CHECK (net_amount = gross_amount - platform_fee)` in the schema
enforces this. It has caught this exact bug in more systems than anyone admits.

---

## 4. The Go interface

```go
// internal/pledge/ledger.go
type Ledger struct{ log *slog.Logger }

// Every method takes the ambient Queries so it joins the caller's transaction.
// A ledger method that opens its own transaction is a bug — it would let the
// domain write commit while the ledger write rolls back.
func (l *Ledger) RecordPledgeCapture(
    ctx context.Context, q Queries, p *Pledge, gatewayFee int64,
) error {
    txn, err := q.InsertLedgerTransaction(ctx, LedgerTxn{
        Kind: KindPledgeCapture, ReferenceType: "pledge", ReferenceID: p.ID,
        Memo: fmt.Sprintf("capture %s", p.ProviderPaymentID),
    })
    if pgerr.IsUnique(err) { return nil }   // uq_ledger_txn_reference: already recorded
    if err != nil { return err }

    escrow,   _ := q.GetOrCreateAccount(ctx, KindPlatformEscrow, nil)
    campEsc,  _ := q.GetOrCreateAccount(ctx, KindCampaignEscrow, &p.CampaignID)
    feeExpense,_ := q.GetOrCreateAccount(ctx, KindGatewayFeeExpense, nil)

    entries := []Entry{
        {txn.ID, escrow.ID,     Debit,  p.Amount},
        {txn.ID, campEsc.ID,    Credit, p.Amount},
    }
    if gatewayFee > 0 {
        entries = append(entries,
            Entry{txn.ID, feeExpense.ID, Debit,  gatewayFee},
            Entry{txn.ID, escrow.ID,     Credit, gatewayFee},
        )
    }
    return q.InsertLedgerEntries(ctx, entries)   // deferred trigger asserts sum == 0 at COMMIT
}
```

Two design points:

1. **`pgerr.IsUnique(err) → return nil`** makes the ledger idempotent at the
   application level too. Combined with `uq_ledger_txn_reference`, replaying a
   capture cannot produce a second set of entries.
2. **`GetOrCreateAccount`** uses `INSERT ... ON CONFLICT (kind, owner_id, currency)
   DO UPDATE SET kind = EXCLUDED.kind RETURNING id` — the pointless `DO UPDATE`
   is there because `DO NOTHING` returns no row on conflict, which then needs a
   second `SELECT`. This is a small Postgres idiom that saves a round trip and
   a race.

---

## 5. Balances

Never store a balance. Compute it:

```sql
CREATE VIEW ledger_balances AS
SELECT a.id, a.kind, a.owner_id,
       COALESCE(SUM(CASE WHEN e.direction = 'DEBIT'  THEN e.amount ELSE 0 END), 0)
     - COALESCE(SUM(CASE WHEN e.direction = 'CREDIT' THEN e.amount ELSE 0 END), 0)
       AS balance
  FROM ledger_accounts a
  LEFT JOIN ledger_entries e ON e.account_id = a.id
 GROUP BY a.id;
```

`balance` is signed from the debit side, so:

- `PLATFORM_ESCROW.balance` is positive (an asset we hold).
- `CAMPAIGN_ESCROW.balance` is **negative** when the campaign holds money — it's
  a credit-normal account. Present it as `-balance` in any UI, and name the
  helper `AmountHeldFor(campaign)` so nobody has to remember the sign.

If the entry count ever makes this view slow (it won't below a few million
rows), add a materialised `ledger_account_snapshots` table with a
`last_entry_id` watermark and compute incrementally. Do that when profiling says
to, not before.

---

## 6. Reconciliation

Nightly, in `cmd/scheduler`. Results land in a `reconciliation_runs` table and
are surfaced at `GET /admin/reconciliation/latest`.

| Check | Query | If it fails |
| --- | --- | --- |
| **R1** every transaction balances | group `ledger_entries` by `transaction_id`, assert signed sum = 0 | CRITICAL. Should be impossible (deferred trigger). If it fires, someone disabled the trigger or wrote entries outside the service. |
| **R2** `campaigns.raised_amount` = Σ captured pledges | see [02 §9](02-DATA-MODEL-POSTGRES.md#9-invariants-to-test) I2 | HIGH. Trust the sum; log the delta; do not auto-correct silently. |
| **R3** `CAMPAIGN_ESCROW` = Σ captured − Σ refunded − payouts, per campaign | derived | HIGH |
| **R4** settled campaigns have zero escrow | `balance <> 0 AND campaign.status = 'RELEASED' AND payout.status = 'PAID'` | HIGH |
| **R5** no negative escrow anywhere | `balance` sign check per account kind | CRITICAL — means money left that never arrived |
| **R6** Σ `CAMPAIGN_ESCROW` + Σ `CREATOR_PAYABLE` + Σ `BACKER_REFUND_PAYABLE` = `PLATFORM_ESCROW` | the master identity | CRITICAL |
| **R7** gateway settlement match | Razorpay settlement report vs `GATEWAY_FEE_EXPENSE` + captures for the period | MEDIUM — small timing differences are normal, large ones are not |

**R6 is the one worth understanding.** Everything the platform holds must be
attributable to someone: a campaign still running, a creator awaiting payout, or
a backer awaiting refund. If the identity doesn't hold, money exists in the
system that belongs to nobody — which means a bug created or destroyed it.

```sql
-- R6
WITH b AS (SELECT kind, SUM(balance) AS total FROM ledger_balances GROUP BY kind)
SELECT
  (SELECT total FROM b WHERE kind = 'PLATFORM_ESCROW') AS held,
  -(COALESCE((SELECT total FROM b WHERE kind = 'CAMPAIGN_ESCROW'), 0)
  + COALESCE((SELECT total FROM b WHERE kind = 'CREATOR_PAYABLE'), 0)
  + COALESCE((SELECT total FROM b WHERE kind = 'BACKER_REFUND_PAYABLE'), 0)) AS owed;
-- held must equal owed
```

**Never auto-correct.** Write an `ADJUSTMENT` transaction only with an admin's
id and a memo explaining what happened. An automatic fix hides the bug that
caused the drift, and the drift will come back bigger.

---

## 7. Tests

| # | Scenario | Assertion |
| --- | --- | --- |
| L1 | Single capture | 4 entries, balanced; escrow = amount; campaign escrow = −amount |
| L2 | Replay the same capture | still 1 transaction, 4 entries (unique constraint holds) |
| L3 | Capture then refund initiated then processed | 3 transactions; campaign escrow 0; refund payable 0; gateway expense unchanged |
| L4 | 100 captures then full payout | campaign escrow exactly 0; fee revenue = floor(raised × 7 / 100) |
| L5 | Deliberately unbalanced insert | transaction **fails at COMMIT** with the trigger's exception |
| L6 | Fee rounding on an amount ending in odd paise | `net = gross − fee` exactly; `chk_payout_math` passes |
| L7 | R6 identity after a randomised sequence of 500 captures/refunds/payouts | holds exactly |

L5 is the test that proves the safety net works. Write it first — if it passes
before the trigger exists, the trigger isn't wired up.

L7 is worth writing as a property test: generate a random valid sequence of
operations, apply them, assert R6. It finds the case you didn't think of.
