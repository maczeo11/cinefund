DROP FUNCTION IF EXISTS set_updated_at();
-- Extensions are left in place: dropping them is not safely reversible if any
-- other schema in the database depends on them.
