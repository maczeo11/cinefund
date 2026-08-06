DROP VIEW IF EXISTS ledger_balances;
DROP TRIGGER IF EXISTS trg_ledger_balanced ON ledger_entries;
DROP FUNCTION IF EXISTS assert_ledger_balanced();
DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS ledger_transactions;
DROP TABLE IF EXISTS ledger_accounts;
