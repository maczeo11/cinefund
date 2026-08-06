# 02 — Postgres Data Model

Postgres owns everything where being wrong costs money. This document is the
authoritative schema: type it into `migrations/` verbatim.

**Conventions used throughout:**

- Money is `BIGINT` **paise**, never `NUMERIC`, never float. `CHECK (amount > 0)`.
- Timestamps are `TIMESTAMPTZ`, always stored UTC, always named `*_at`.
- Primary keys are `UUID` generated in Go (`uuid.NewV7()` — time-ordered, so
  B-tree inserts stay at the right edge of the index instead of scattering).
- Enums are `TEXT` + `CHECK`, not Postgres `ENUM` types. Adding a value to a
  native enum requires `ALTER TYPE`, which doesn't run inside a transaction on
  older versions and can't be rolled back cleanly. A `CHECK` is one `ALTER TABLE`.
- Every table has `created_at`; mutable tables also have `updated_at` maintained
  by a trigger, not by application code that will eventually forget.

---

## 0. Extensions and shared helpers

```sql
-- migrations/0001_init.up.sql
CREATE EXTENSION IF NOT EXISTS "pgcrypto";   -- gen_random_uuid() as a fallback
CREATE EXTENSION IF NOT EXISTS "citext";     -- case-insensitive email

CREATE OR REPLACE FUNCTION set_updated_at() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
```

Apply to every mutable table:

```sql
CREATE TRIGGER trg_<table>_updated
    BEFORE UPDATE ON <table>
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
```

---

## 1. Identity

### `users`

```sql
CREATE TABLE users (
    id              UUID PRIMARY KEY,
    email           CITEXT NOT NULL UNIQUE,
    password_hash   TEXT   NOT NULL,
    display_name    TEXT   NOT NULL CHECK (length(display_name) BETWEEN 2 AND 60),
    role            TEXT   NOT NULL DEFAULT 'USER'
                           CHECK (role IN ('USER','ADMIN')),
    email_verified  BOOLEAN NOT NULL DEFAULT FALSE,
    avatar_key      TEXT,                     -- object storage key, nullable
    status          TEXT   NOT NULL DEFAULT 'ACTIVE'
                           CHECK (status IN ('ACTIVE','SUSPENDED','DELETED')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

`CITEXT` on email means `Bhanu@x.com` and `bhanu@x.com` collide at the unique
index — which is what users expect and what stops duplicate-account confusion.

**Registration always forces `role = 'USER'`.** Never bind `role` from the
request body. This is the exact bug class you caught in PRAJNA; the schema
default plus an explicit insert column list is the structural fix.

### `refresh_token_families`

Refresh rotation with reuse detection needs a *family*, not a token list.

```sql
CREATE TABLE refresh_token_families (
    id           UUID PRIMARY KEY,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    current_jti  UUID NOT NULL,                 -- the only jti currently valid
    user_agent   TEXT,
    ip           INET,
    revoked_at   TIMESTAMPTZ,                   -- non-null = family burned
    revoke_reason TEXT CHECK (revoke_reason IN ('LOGOUT','REUSE_DETECTED','ADMIN','EXPIRED')),
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_rtf_user_active ON refresh_token_families (user_id)
    WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX idx_rtf_current_jti ON refresh_token_families (current_jti);
```

The mechanism, in full, because it's subtle:

1. Login creates a family with `current_jti = J1`; the refresh JWT carries
   `family_id` and `jti = J1`.
2. Refresh with `J1`: verify `family.current_jti = J1` and `revoked_at IS NULL`,
   then rotate to `J2` in one `UPDATE ... WHERE current_jti = J1` (the `WHERE`
   makes it a compare-and-swap — two concurrent refreshes, one wins).
3. Refresh with `J1` **again**: `current_jti` is now `J2`, so the update affects
   zero rows. That's a **reuse**: either a stolen token or a race. Burn the
   entire family (`revoked_at = now(), revoke_reason = 'REUSE_DETECTED'`) and
   return 401. The attacker and the victim are both logged out — correct, because
   you can't tell which is which.

### `creator_profiles`

```sql
CREATE TABLE creator_profiles (
    id            UUID PRIMARY KEY,
    user_id       UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    bio           TEXT NOT NULL CHECK (length(bio) <= 2000),
    portfolio_url TEXT,
    status        TEXT NOT NULL DEFAULT 'PENDING'
                       CHECK (status IN ('PENDING','APPROVED','REJECTED')),
    review_note   TEXT,
    reviewed_by   UUID REFERENCES users(id),
    reviewed_at   TIMESTAMPTZ,
    payout_upi    TEXT,                        -- v1 payouts are manual; store the handle only
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

---

## 2. Campaigns

### `campaigns`

```sql
CREATE TABLE campaigns (
    id             UUID PRIMARY KEY,
    creator_id     UUID NOT NULL REFERENCES users(id),
    slug           TEXT NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{2,79}$'),
    title          TEXT NOT NULL CHECK (length(title) BETWEEN 3 AND 120),
    tagline        TEXT NOT NULL CHECK (length(tagline) <= 200),
    synopsis       TEXT NOT NULL,
    risks          TEXT,
    category       TEXT NOT NULL CHECK (category IN
                        ('DRAMA','COMEDY','DOCUMENTARY','ANIMATION','HORROR','SCIFI','EXPERIMENTAL')),
    language       TEXT NOT NULL DEFAULT 'en',

    goal_amount    BIGINT NOT NULL CHECK (goal_amount >= 100000),   -- min ₹1,000
    raised_amount  BIGINT NOT NULL DEFAULT 0 CHECK (raised_amount >= 0),
    backer_count   INTEGER NOT NULL DEFAULT 0 CHECK (backer_count >= 0),
    currency       TEXT NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),

    status         TEXT NOT NULL DEFAULT 'DRAFT' CHECK (status IN
                        ('DRAFT','IN_REVIEW','LIVE','FUNDED','FAILED','CANCELLED',
                         'IN_PRODUCTION','RELEASED')),
    duration_days  INTEGER CHECK (duration_days BETWEEN 7 AND 90),
    published_at   TIMESTAMPTZ,
    deadline       TIMESTAMPTZ,                 -- resolved at publish: published_at + duration_days
    finalized_at   TIMESTAMPTZ,                 -- when FUNDED/FAILED was decided

    cover_key      TEXT,
    pitch_asset_id TEXT,                        -- points into object storage / the media asset row, deliberately not an FK
    review_note    TEXT,

    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_live_has_deadline
        CHECK (status <> 'LIVE' OR (deadline IS NOT NULL AND published_at IS NOT NULL))
);

CREATE INDEX idx_campaigns_status_deadline ON campaigns (status, deadline)
    WHERE status = 'LIVE';                       -- the deadline sweep's index
CREATE INDEX idx_campaigns_creator     ON campaigns (creator_id, created_at DESC);
CREATE INDEX idx_campaigns_category    ON campaigns (category, status);
```

Note `pitch_asset_id TEXT` with no foreign key: it points at a media asset whose
metadata lives in a `JSONB` column on the same database (see the media tables).
The app enforces the linkage; there is no second store to keep in sync.

The partial index `WHERE status = 'LIVE'` is deliberate — the deadline sweep runs
every 60 seconds forever, and it should touch an index containing only live
campaigns, not all campaigns ever created.

### `reward_tiers`

```sql
CREATE TABLE reward_tiers (
    id              UUID PRIMARY KEY,
    campaign_id     UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    title           TEXT NOT NULL CHECK (length(title) BETWEEN 2 AND 80),
    description     TEXT NOT NULL,
    min_amount      BIGINT NOT NULL CHECK (min_amount >= 10000),   -- min ₹100
    quantity_limit  INTEGER CHECK (quantity_limit > 0),            -- NULL = unlimited
    claimed_count   INTEGER NOT NULL DEFAULT 0 CHECK (claimed_count >= 0),
    estimated_delivery DATE,
    grants_download BOOLEAN NOT NULL DEFAULT FALSE,
    grants_credit   BOOLEAN NOT NULL DEFAULT FALSE,
    grants_bts      BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order      INTEGER NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_tier_not_oversold
        CHECK (quantity_limit IS NULL OR claimed_count <= quantity_limit)
);

CREATE INDEX idx_tiers_campaign ON reward_tiers (campaign_id, sort_order);
```

`chk_tier_not_oversold` is the one that matters. Rule F4 (a limited tier can't be
over-claimed) is enforced **by the database**, not by a `if claimed < limit` in
Go. Under concurrency the Go check races; the constraint cannot. The service
still does `SELECT ... FOR UPDATE` on the tier first so the common case gets a
clean error rather than a constraint violation — but the constraint is what makes
it *correct*.

---

## 3. Pledges and payments

### `pledges`

```sql
CREATE TABLE pledges (
    id                UUID PRIMARY KEY,
    campaign_id       UUID NOT NULL REFERENCES campaigns(id),
    backer_id         UUID NOT NULL REFERENCES users(id),
    tier_id           UUID REFERENCES reward_tiers(id),
    amount            BIGINT NOT NULL CHECK (amount > 0),
    currency          TEXT NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
    anonymous         BOOLEAN NOT NULL DEFAULT FALSE,
    message           TEXT CHECK (length(message) <= 500),

    status            TEXT NOT NULL DEFAULT 'CREATED' CHECK (status IN
                          ('CREATED','AUTHORIZED','CAPTURED','FAILED',
                           'REFUND_PENDING','REFUNDED','REFUND_FAILED','SETTLED')),

    provider          TEXT NOT NULL DEFAULT 'razorpay',
    provider_order_id TEXT UNIQUE,
    provider_payment_id TEXT UNIQUE,

    captured_at       TIMESTAMPTZ,
    refunded_at       TIMESTAMPTZ,
    failure_reason    TEXT,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_captured_has_payment
        CHECK (status NOT IN ('CAPTURED','SETTLED') OR provider_payment_id IS NOT NULL),
    CONSTRAINT chk_captured_has_time
        CHECK (status NOT IN ('CAPTURED','SETTLED') OR captured_at IS NOT NULL)
);

CREATE INDEX idx_pledges_campaign_status ON pledges (campaign_id, status);
CREATE INDEX idx_pledges_backer          ON pledges (backer_id, created_at DESC);
CREATE INDEX idx_pledges_stale           ON pledges (created_at)
    WHERE status = 'CREATED';                    -- reconciliation job's index
```

`provider_payment_id UNIQUE` is quietly one of the most important constraints in
the schema: **one Razorpay payment can be attached to at most one pledge, ever.**
If a bug or a replayed webhook tries to attach the same payment twice, the insert
fails rather than double-crediting.

The partial index on stale `CREATED` pledges powers the reconciliation sweep in
[06](06-PAYMENTS-RAZORPAY.md#reconciliation): anything sitting in `CREATED` for
more than 15 minutes gets its true status pulled from Razorpay's API.

### `payment_events`

Every webhook the system has ever accepted. This is the durable idempotency
ledger and the audit trail.

```sql
CREATE TABLE payment_events (
    id                 UUID PRIMARY KEY,
    provider           TEXT NOT NULL DEFAULT 'razorpay',
    provider_event_id  TEXT NOT NULL,
    event_type         TEXT NOT NULL,          -- payment.captured, refund.processed, ...
    pledge_id          UUID REFERENCES pledges(id),
    payload            JSONB NOT NULL,
    signature_valid    BOOLEAN NOT NULL,
    processed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_provider_event UNIQUE (provider, provider_event_id)
);

CREATE INDEX idx_payment_events_pledge ON payment_events (pledge_id, processed_at DESC);
CREATE INDEX idx_payment_events_type   ON payment_events (event_type, processed_at DESC);
```

**`uq_provider_event` is the real idempotency guarantee.** Redis `SETNX` is a
performance optimisation in front of it. The processing transaction inserts here
*first*; a `23505` unique violation means "already processed" and the whole
transaction rolls back harmlessly.

Store the full `payload` — when a payment dispute arrives six months later, the
exact bytes the provider sent are the only thing that settles it. Redact nothing
except any card-ish fields (Razorpay doesn't send PANs, but assert it in code).

### `refunds`

```sql
CREATE TABLE refunds (
    id                 UUID PRIMARY KEY,
    pledge_id          UUID NOT NULL REFERENCES pledges(id),
    amount             BIGINT NOT NULL CHECK (amount > 0),
    reason             TEXT NOT NULL CHECK (reason IN
                           ('CAMPAIGN_FAILED','CAMPAIGN_CANCELLED','BACKER_CANCELLED','ADMIN')),
    status             TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN
                           ('PENDING','PROCESSING','COMPLETED','FAILED')),
    provider_refund_id TEXT UNIQUE,
    idempotency_key    TEXT NOT NULL UNIQUE,   -- sent to Razorpay, derived from pledge_id
    failure_reason     TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_refund_active_per_pledge ON refunds (pledge_id)
    WHERE status <> 'FAILED';
```

That partial unique index is the guard against double-refunding: **at most one
non-failed refund per pledge**. A failed refund can be retried (a new row); a
completed one blocks any further attempt. Getting this wrong means paying a
backer twice, which nobody will ever tell you about.

### `payouts`

```sql
CREATE TABLE payouts (
    id            UUID PRIMARY KEY,
    campaign_id   UUID NOT NULL REFERENCES campaigns(id),
    creator_id    UUID NOT NULL REFERENCES users(id),
    gross_amount  BIGINT NOT NULL CHECK (gross_amount > 0),
    platform_fee  BIGINT NOT NULL CHECK (platform_fee >= 0),
    net_amount    BIGINT NOT NULL CHECK (net_amount > 0),
    status        TEXT NOT NULL DEFAULT 'REQUESTED' CHECK (status IN
                      ('REQUESTED','APPROVED','PAID','REJECTED')),
    reference     TEXT,                        -- bank/UPI reference, entered by admin in v1
    approved_by   UUID REFERENCES users(id),
    paid_at       TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_payout_math CHECK (net_amount = gross_amount - platform_fee)
);

CREATE UNIQUE INDEX uq_payout_per_campaign ON payouts (campaign_id)
    WHERE status <> 'REJECTED';
```

`chk_payout_math` is free arithmetic insurance. It costs nothing and catches the
day someone changes the fee calculation and forgets one branch.

---

## 4. Ledger

Full treatment in [07](07-LEDGER.md); the schema lives here.

```sql
CREATE TABLE ledger_accounts (
    id           UUID PRIMARY KEY,
    kind         TEXT NOT NULL CHECK (kind IN
                     ('PLATFORM_ESCROW','CAMPAIGN_ESCROW','CREATOR_PAYABLE',
                      'BACKER_REFUND_PAYABLE','PLATFORM_FEE_REVENUE','GATEWAY_FEE_EXPENSE')),
    owner_id     UUID,                          -- campaign_id / user_id / NULL for platform
    currency     TEXT NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_account UNIQUE (kind, owner_id, currency)
);

CREATE TABLE ledger_transactions (
    id            UUID PRIMARY KEY,
    kind          TEXT NOT NULL CHECK (kind IN
                      ('PLEDGE_CAPTURE','REFUND','PAYOUT','FEE','ADJUSTMENT')),
    reference_type TEXT NOT NULL,               -- 'pledge' | 'refund' | 'payout'
    reference_id  UUID NOT NULL,
    memo          TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_ledger_txn_reference UNIQUE (kind, reference_type, reference_id)
);

CREATE TABLE ledger_entries (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transaction_id UUID NOT NULL REFERENCES ledger_transactions(id),
    account_id     UUID NOT NULL REFERENCES ledger_accounts(id),
    direction      TEXT NOT NULL CHECK (direction IN ('DEBIT','CREDIT')),
    amount         BIGINT NOT NULL CHECK (amount > 0),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_entries_txn     ON ledger_entries (transaction_id);
CREATE INDEX idx_entries_account ON ledger_entries (account_id, created_at DESC);
```

`uq_ledger_txn_reference` makes the ledger idempotent at the schema level: you
cannot record two `PLEDGE_CAPTURE` transactions for the same pledge. Combined
with `payment_events.uq_provider_event`, double-crediting requires two
independent constraints to fail simultaneously.

### Balance enforcement

Every transaction's entries must sum to zero (debits = credits). Enforce it with
a deferred constraint trigger so it's checked at `COMMIT`, after all entries in
the transaction have been inserted:

```sql
CREATE OR REPLACE FUNCTION assert_ledger_balanced() RETURNS TRIGGER AS $$
DECLARE
    imbalance BIGINT;
BEGIN
    SELECT COALESCE(SUM(CASE WHEN direction = 'DEBIT' THEN amount ELSE -amount END), 0)
      INTO imbalance
      FROM ledger_entries
     WHERE transaction_id = NEW.transaction_id;

    IF imbalance <> 0 THEN
        RAISE EXCEPTION 'ledger transaction % is unbalanced by % paise',
            NEW.transaction_id, imbalance;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER trg_ledger_balanced
    AFTER INSERT ON ledger_entries
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION assert_ledger_balanced();
```

`DEFERRABLE INITIALLY DEFERRED` is the whole trick — without it the trigger fires
after the first entry, when the transaction is legitimately unbalanced.

---

## 5. Entitlements

```sql
CREATE TABLE entitlements (
    id          UUID PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    film_id     TEXT NOT NULL,                  -- = campaign_id; films are 1:1 with campaigns
    kind        TEXT NOT NULL CHECK (kind IN ('EARLY_ACCESS','DOWNLOAD','CREDIT','BTS')),
    source_pledge_id UUID REFERENCES pledges(id),
    granted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ,                    -- NULL = permanent
    revoked_at  TIMESTAMPTZ,

    CONSTRAINT uq_entitlement UNIQUE (user_id, film_id, kind)
);

CREATE INDEX idx_entitlements_lookup ON entitlements (user_id, film_id)
    WHERE revoked_at IS NULL;
```

The authorisation query is a single indexed lookup:

```sql
SELECT 1 FROM entitlements
 WHERE user_id = $1 AND film_id = $2 AND kind = $3
   AND revoked_at IS NULL
   AND (expires_at IS NULL OR expires_at > now())
 LIMIT 1;
```

Never cache this result. A revoked entitlement must take effect on the next
request, and a 5-minute cache on an authorisation decision is a 5-minute
security hole.

---

## 6. Outbox

```sql
CREATE TABLE outbox (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id       UUID NOT NULL UNIQUE,
    event_type     TEXT NOT NULL,               -- 'pledge.captured'
    event_version  INTEGER NOT NULL DEFAULT 1,
    aggregate_type TEXT NOT NULL,               -- 'pledge'
    aggregate_id   UUID NOT NULL,               -- Kafka partition key
    payload        JSONB NOT NULL,
    trace_id       TEXT,                        -- W3C traceparent, for cross-process tracing
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at   TIMESTAMPTZ,
    attempts       INTEGER NOT NULL DEFAULT 0,
    last_error     TEXT
);

CREATE INDEX idx_outbox_unpublished ON outbox (id)
    WHERE published_at IS NULL;
```

A `BIGINT` identity PK, not a UUID, because the dispatcher reads **in insertion
order** and a monotonic integer is the cheapest way to get that. `event_id` is
the UUID that travels to Kafka and is what consumers dedupe on.

The partial index is essential: the table grows forever (until the retention job
prunes published rows older than 7 days), but the dispatcher's query only ever
scans unpublished rows, which is normally a handful.

The dispatcher's claim query:

```sql
SELECT id, event_id, event_type, aggregate_type, aggregate_id, payload, trace_id
  FROM outbox
 WHERE published_at IS NULL
 ORDER BY id
   FOR UPDATE SKIP LOCKED
 LIMIT 100;
```

`SKIP LOCKED` is what lets you run N dispatcher replicas with zero coordination —
each grabs a different batch instead of blocking on the same rows.

---

## 7. Idempotency keys (client-supplied)

Distinct from webhook idempotency. This covers "the mobile client retried
`POST /pledges` because the response timed out".

```sql
CREATE TABLE idempotency_keys (
    key           TEXT NOT NULL,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint      TEXT NOT NULL,
    request_hash  TEXT NOT NULL,                -- SHA-256 of the canonical request body
    status        TEXT NOT NULL DEFAULT 'IN_FLIGHT'
                       CHECK (status IN ('IN_FLIGHT','COMPLETED')),
    response_code INTEGER,
    response_body JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (user_id, endpoint, key)
);

CREATE INDEX idx_idem_expiry ON idempotency_keys (expires_at);
```

Scoping the key by `user_id` is a security property, not tidiness: a global key
namespace lets user A guess user B's key and read B's cached response body.

`request_hash` catches the other failure: same key, *different* body. That's a
client bug and must return `422`, not the first response.

---

## 8. Audit log

```sql
CREATE TABLE audit_log (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id    UUID REFERENCES users(id),
    actor_role  TEXT NOT NULL,
    action      TEXT NOT NULL,                  -- 'campaign.approve', 'refund.manual'
    target_type TEXT NOT NULL,
    target_id   TEXT NOT NULL,
    before      JSONB,
    after       JSONB,
    ip          INET,
    request_id  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_target ON audit_log (target_type, target_id, created_at DESC);
CREATE INDEX idx_audit_actor  ON audit_log (actor_id, created_at DESC);
```

Every admin action and every money movement writes here, in the same transaction
as the change itself. Append-only: revoke `UPDATE`/`DELETE` from the application
role.

---

## 9. Invariants to test

Turn each of these into a SQL assertion the reconciliation job runs nightly and
a test asserts after every integration scenario. If one fails, something is
genuinely wrong and you want to know within a day, not a quarter.

| # | Invariant | Query |
| --- | --- | --- |
| I1 | Every ledger transaction balances | `SELECT transaction_id FROM ledger_entries GROUP BY transaction_id HAVING SUM(CASE WHEN direction='DEBIT' THEN amount ELSE -amount END) <> 0` |
| I2 | `campaigns.raised_amount` equals the sum of captured pledges | `SELECT c.id FROM campaigns c LEFT JOIN pledges p ON p.campaign_id=c.id AND p.status IN ('CAPTURED','SETTLED') GROUP BY c.id, c.raised_amount HAVING COALESCE(SUM(p.amount),0) <> c.raised_amount` |
| I3 | No tier is oversold | `SELECT id FROM reward_tiers WHERE quantity_limit IS NOT NULL AND claimed_count > quantity_limit` |
| I4 | No pledge is CAPTURED without a payment id | covered by `chk_captured_has_payment` |
| I5 | No pledge has more than one non-failed refund | covered by `uq_refund_active_per_pledge` |
| I6 | Campaign escrow balance ≥ 0 for every campaign | sum entries per `CAMPAIGN_ESCROW` account |
| I7 | No `LIVE` campaign is past its deadline by more than 5 minutes | `SELECT id FROM campaigns WHERE status='LIVE' AND deadline < now() - interval '5 minutes'` |
| I8 | Outbox lag is bounded | `SELECT max(now() - created_at) FROM outbox WHERE published_at IS NULL` |

I7 is the one that catches a dead scheduler, and it's the failure you'd
otherwise notice only when a creator emails asking why their campaign never
closed.

---

## 10. Migration file plan

Forward-only, numbered, one concern each. Never edit a committed migration.

```
migrations/
├── 0001_extensions_and_helpers.up.sql
├── 0002_users.up.sql
├── 0003_refresh_token_families.up.sql
├── 0004_creator_profiles.up.sql
├── 0005_campaigns.up.sql
├── 0006_reward_tiers.up.sql
├── 0007_pledges.up.sql
├── 0008_payment_events.up.sql
├── 0009_refunds.up.sql
├── 0010_payouts.up.sql
├── 0011_ledger.up.sql
├── 0012_entitlements.up.sql
├── 0013_outbox.up.sql
├── 0014_idempotency_keys.up.sql
├── 0015_audit_log.up.sql
└── (matching .down.sql for each)
```

Use `golang-migrate` or `goose`. Run migrations as a **separate binary in a
separate step** (`cmd/migrate`), never on API startup — otherwise N replicas
racing to migrate on deploy is your next incident.
