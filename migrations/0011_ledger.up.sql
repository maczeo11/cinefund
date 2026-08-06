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
    id             UUID PRIMARY KEY,
    kind           TEXT NOT NULL CHECK (kind IN
                       ('PLEDGE_CAPTURE','REFUND','PAYOUT','FEE','ADJUSTMENT')),
    reference_type TEXT NOT NULL,               -- 'pledge' | 'refund' | 'payout'
    reference_id   UUID NOT NULL,
    memo           TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_ledger_txn_reference UNIQUE (kind, reference_type, reference_id)
);

-- uq_ledger_txn_reference makes the ledger idempotent at the schema level: you
-- cannot record two PLEDGE_CAPTURE transactions for the same pledge. Combined
-- with payment_events.uq_provider_event, double-crediting requires two
-- independent constraints to fail at the same time.

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

-- ---------------------------------------------------------------------------
-- Balance enforcement.
--
-- Every transaction's entries must sum to zero. DEFERRABLE INITIALLY DEFERRED
-- is the whole trick: without it the trigger fires after the first entry, when
-- the transaction is legitimately unbalanced.
--
-- Test L5 (docs/07 §7) writes a deliberately unbalanced pair and asserts the
-- COMMIT fails. Write that test BEFORE this trigger exists, watch it pass
-- wrongly, then add the trigger. That is how you know the net is wired up
-- rather than assumed.
-- ---------------------------------------------------------------------------
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

-- Balances are never stored. Compute them.
CREATE VIEW ledger_balances AS
SELECT a.id, a.kind, a.owner_id,
       COALESCE(SUM(CASE WHEN e.direction = 'DEBIT'  THEN e.amount ELSE 0 END), 0)
     - COALESCE(SUM(CASE WHEN e.direction = 'CREDIT' THEN e.amount ELSE 0 END), 0)
       AS balance
  FROM ledger_accounts a
  LEFT JOIN ledger_entries e ON e.account_id = a.id
 GROUP BY a.id;

-- NOTE the sign convention: balance is computed from the debit side, so
-- CAMPAIGN_ESCROW is NEGATIVE while the campaign is holding money (it is a
-- credit-normal account). Present it as -balance in any UI.
