-- Restores the original constraint. Accounts merged by the up migration stay
-- merged: there is no record of which duplicate each entry came from.
ALTER TABLE ledger_accounts DROP CONSTRAINT uq_account;
ALTER TABLE ledger_accounts ADD CONSTRAINT uq_account UNIQUE (kind, owner_id, currency);
