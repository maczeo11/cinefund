CREATE TABLE users (
    id              UUID PRIMARY KEY,
    email           CITEXT NOT NULL UNIQUE,
    password_hash   TEXT   NOT NULL,
    display_name    TEXT   NOT NULL CHECK (length(display_name) BETWEEN 2 AND 60),
    role            TEXT   NOT NULL DEFAULT 'USER'
                           CHECK (role IN ('USER','ADMIN')),
    email_verified  BOOLEAN NOT NULL DEFAULT FALSE,
    avatar_key      TEXT,
    status          TEXT   NOT NULL DEFAULT 'ACTIVE'
                           CHECK (status IN ('ACTIVE','SUSPENDED','DELETED')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- CITEXT means Bhanu@x.com and bhanu@x.com collide at the unique index, which
-- is what users expect and what stops duplicate-account confusion.

CREATE TRIGGER trg_users_updated
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
