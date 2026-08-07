package pledge

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo is the Postgres implementation.
type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// Bound wraps a tx as Queries.
func (r *Repo) Bound(tx pgx.Tx) Queries { return &pgQueries{tx: tx} }

// Do runs fn in a transaction.
func (r *Repo) Do(ctx context.Context, fn func(q Queries) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(r.Bound(tx)); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repo) AttachOrder(ctx context.Context, pledgeID uuid.UUID, orderID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE pledges SET provider_order_id = $1 WHERE id = $2`, orderID, pledgeID)
	return err
}

// pgQueries wraps a single tx.
type pgQueries struct {
	tx pgx.Tx
}

func (q *pgQueries) InsertPledge(ctx context.Context, p *Pledge) error {
	_, err := q.tx.Exec(ctx, `
		INSERT INTO pledges (id, campaign_id, backer_id, tier_id, amount, currency,
		                     anonymous, message, status, provider_order_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		p.ID, p.CampaignID, p.BackerID, p.TierID, p.Amount, p.Currency,
		p.Anonymous, p.Message, p.Status, p.ProviderOrderID)
	return err
}

func (q *pgQueries) GetPledgeForUpdate(ctx context.Context, id uuid.UUID) (*Pledge, error) {
	row := q.tx.QueryRow(ctx, `
		SELECT id, campaign_id, backer_id, tier_id, amount, currency, anonymous,
		       message, status, COALESCE(provider_order_id,''), COALESCE(provider_payment_id,''),
		       captured_at, refunded_at, created_at
		  FROM pledges WHERE id = $1 FOR UPDATE`, id)
	return scanPledge(row)
}

func (q *pgQueries) GetPledgeByOrderID(ctx context.Context, orderID string) (*Pledge, error) {
	row := q.tx.QueryRow(ctx, `
		SELECT id, campaign_id, backer_id, tier_id, amount, currency, anonymous,
		       message, status, COALESCE(provider_order_id,''), COALESCE(provider_payment_id,''),
		       captured_at, refunded_at, created_at
		  FROM pledges WHERE provider_order_id = $1 FOR UPDATE`, orderID)
	return scanPledge(row)
}

func (q *pgQueries) GetCampaignForUpdate(ctx context.Context, id uuid.UUID) (*Campaign, error) {
	row := q.tx.QueryRow(ctx, `
		SELECT id, creator_id, status, deadline FROM campaigns
		 WHERE id = $1 FOR UPDATE`, id)
	var c Campaign
	err := row.Scan(&c.ID, &c.CreatorID, &c.Status, &c.Deadline)
	return &c, err
}

func (q *pgQueries) GetTierForUpdate(ctx context.Context, id uuid.UUID) (*Tier, error) {
	row := q.tx.QueryRow(ctx, `
		SELECT id, campaign_id, min_amount, quantity_limit, claimed_count
		  FROM reward_tiers WHERE id = $1 FOR UPDATE`, id)
	var t Tier
	var qty *int
	err := row.Scan(&t.ID, &t.CampaignID, &t.MinAmount, &qty, &t.ClaimedCount)
	if err != nil {
		return nil, err
	}
	t.QuantityLimit = qty
	return &t, nil
}

func (q *pgQueries) MarkPledgeCaptured(ctx context.Context, id uuid.UUID, paymentID string, at time.Time) error {
	_, err := q.tx.Exec(ctx, `
		UPDATE pledges SET status='CAPTURED', provider_payment_id=$2, captured_at=$3
		 WHERE id=$1`, id, paymentID, at)
	return err
}

func (q *pgQueries) SetPledgeStatus(ctx context.Context, id uuid.UUID, s Status) error {
	_, err := q.tx.Exec(ctx, `UPDATE pledges SET status=$2 WHERE id=$1`, id, s)
	return err
}

func (q *pgQueries) IncrementCampaignRaised(ctx context.Context, campaignID uuid.UUID, amount int64) error {
	_, err := q.tx.Exec(ctx, `
		UPDATE campaigns SET raised_amount = raised_amount + $2, backer_count = backer_count + 1
		 WHERE id = $1`, campaignID, amount)
	return err
}

func (q *pgQueries) IncrementTierClaimed(ctx context.Context, tierID uuid.UUID) error {
	_, err := q.tx.Exec(ctx, `
		UPDATE reward_tiers SET claimed_count = claimed_count + 1 WHERE id = $1`, tierID)
	return err
}

func (q *pgQueries) InsertPaymentEvent(ctx context.Context, evt PaymentEvent) error {
	_, err := q.tx.Exec(ctx, `
		INSERT INTO payment_events (id, provider, provider_event_id, event_type, pledge_id,
		                            payload, signature_valid)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		evt.ID, evt.Provider, evt.ProviderEventID, evt.EventType, evt.PledgeID,
		evt.Payload, evt.SignatureValid)
	return err
}

func (q *pgQueries) InsertLedgerTransaction(ctx context.Context, t LedgerTxn) (uuid.UUID, error) {
	id := uuid.New()
	_, err := q.tx.Exec(ctx, `
		INSERT INTO ledger_transactions (id, kind, reference_type, reference_id, memo)
		VALUES ($1,$2,$3,$4,$5)`,
		id, t.Kind, t.ReferenceType, t.ReferenceID, t.Memo)
	if err != nil {
		return uuid.Nil, err
	}
	t.ID = id
	return id, nil
}

func (q *pgQueries) InsertLedgerEntries(ctx context.Context, entries []LedgerEntry) error {
	for _, e := range entries {
		if _, err := q.tx.Exec(ctx, `
			INSERT INTO ledger_entries (transaction_id, account_id, direction, amount)
			VALUES ($1,$2,$3,$4)`,
			e.TransactionID, e.AccountID, e.Direction, e.Amount); err != nil {
			return err
		}
	}
	return nil
}

func (q *pgQueries) GetOrCreateAccount(ctx context.Context, kind AccountKind, ownerID *uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := q.tx.QueryRow(ctx, `
		INSERT INTO ledger_accounts (id, kind, owner_id)
		VALUES (gen_random_uuid(), $1, $2)
		ON CONFLICT (kind, owner_id, currency) DO UPDATE SET kind = EXCLUDED.kind
		RETURNING id`, kind, ownerID).Scan(&id)
	return id, err
}

func (q *pgQueries) InsertOutbox(ctx context.Context, evt OutboxEvent) error {
	_, err := q.tx.Exec(ctx, `
		INSERT INTO outbox (event_id, event_type, event_version, aggregate_type,
		                    aggregate_id, payload, trace_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		evt.ID, evt.Type, evt.Version, evt.AggregateType, evt.AggregateID, evt.Payload, evt.TraceID)
	return err
}

type rowScanner interface{ Scan(dest ...any) error }

func scanPledge(row rowScanner) (*Pledge, error) {
	var p Pledge
	var capturedAt, refundedAt *time.Time
	err := row.Scan(&p.ID, &p.CampaignID, &p.BackerID, &p.TierID, &p.Amount, &p.Currency,
		&p.Anonymous, &p.Message, &p.Status, &p.ProviderOrderID, &p.ProviderPaymentID,
		&capturedAt, &refundedAt, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	p.CapturedAt = capturedAt
	p.RefundedAt = refundedAt
	return &p, nil
}
