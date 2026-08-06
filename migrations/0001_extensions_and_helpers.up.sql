CREATE EXTENSION IF NOT EXISTS "pgcrypto";   -- gen_random_uuid() as a fallback
CREATE EXTENSION IF NOT EXISTS "citext";     -- case-insensitive email

-- Applied to every mutable table via a BEFORE UPDATE trigger, so application
-- code that forgets to set updated_at cannot drift.
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
