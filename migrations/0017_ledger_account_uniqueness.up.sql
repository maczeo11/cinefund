-- uq_account treated every NULL owner_id as distinct, so ON CONFLICT never
-- fired for the platform-wide accounts (PLATFORM_ESCROW, GATEWAY_FEE_EXPENSE,
-- ...). Each capture created a fresh account instead of reusing the one
-- account, spreading the platform's balance across many rows.
--
-- Fold the duplicates into the oldest account of each (kind, owner_id,
-- currency), then recreate the constraint with NULLS NOT DISTINCT (Postgres
-- 15+) so a NULL owner conflicts like any other value. Moving entries between
-- duplicates of the same account leaves every transaction balanced.

-- The same window is computed in both statements (not a temp table) so the
-- file runs correctly with or without a wrapping transaction. The UPDATE does
-- not touch ledger_accounts, so both see the same keep_id for every account.
UPDATE ledger_entries e
   SET account_id = m.keep_id
  FROM (SELECT id,
               first_value(id) OVER (
                   PARTITION BY kind, owner_id, currency
                   ORDER BY created_at, id
               ) AS keep_id
          FROM ledger_accounts) m
 WHERE e.account_id = m.id
   AND m.id <> m.keep_id;

DELETE FROM ledger_accounts a
 USING (SELECT id,
               first_value(id) OVER (
                   PARTITION BY kind, owner_id, currency
                   ORDER BY created_at, id
               ) AS keep_id
          FROM ledger_accounts) m
 WHERE a.id = m.id
   AND m.id <> m.keep_id;

ALTER TABLE ledger_accounts DROP CONSTRAINT uq_account;
ALTER TABLE ledger_accounts
    ADD CONSTRAINT uq_account UNIQUE NULLS NOT DISTINCT (kind, owner_id, currency);
