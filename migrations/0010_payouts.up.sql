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

-- chk_payout_math is free arithmetic insurance: it costs nothing and catches the
-- day someone changes the fee calculation and forgets one branch.

CREATE UNIQUE INDEX uq_payout_per_campaign ON payouts (campaign_id)
    WHERE status <> 'REJECTED';

CREATE TRIGGER trg_payouts_updated
    BEFORE UPDATE ON payouts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
