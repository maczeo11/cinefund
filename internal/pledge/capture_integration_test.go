//go:build integration

package pledge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/maczeo11/cinefund/internal/pledge/gateway/fake"
)

func capturePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://cinefund:cinefund@localhost:5433/cinefund?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect pg: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedLimitedTier inserts a creator, a LIVE campaign and a one-slot tier.
func seedLimitedTier(t *testing.T, pool *pgxpool.Pool) (campaignID, tierID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	creator, campaignID, tierID := uuid.New(), uuid.New(), uuid.New()
	seedUser(t, pool, creator)
	if _, err := pool.Exec(ctx, `
		INSERT INTO campaigns (id, creator_id, slug, title, tagline, synopsis, category,
		                       goal_amount, status, deadline, published_at)
		VALUES ($1,$2,$3,'Integration film','t','s','DRAMA',100000,'LIVE',now()+interval '30 days',now())`,
		campaignID, creator, "it-"+campaignID.String()[:8]); err != nil {
		t.Fatalf("seed campaign: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO reward_tiers (id, campaign_id, title, description, min_amount, quantity_limit)
		VALUES ($1,$2,'Last seat','d',10000,1)`, tierID, campaignID); err != nil {
		t.Fatalf("seed tier: %v", err)
	}
	return campaignID, tierID
}

func seedUser(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, email, password_hash, display_name) VALUES ($1,$2,'','Integration')`,
		id, id.String()+"@example.test"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

// The race that used to strand a payment, run against real Postgres: both
// captures commit, the tier is never oversold, and the second backer's money
// is booked as owed back to them.
func TestCapture_TierSoldOutWhilePaying_Postgres(t *testing.T) {
	pool := capturePool(t)
	ctx := context.Background()
	campaignID, tierID := seedLimitedTier(t, pool)

	repo := NewRepo(pool)
	svc := NewService(repo, repo, NewLedger(), fake.New(), newFakeRedis(),
		Secrets{WebhookSecret: "whsec"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	var pledges []*Pledge
	for i := 0; i < 2; i++ {
		backer := uuid.New()
		seedUser(t, pool, backer)
		p, err := svc.CreatePledge(ctx, CreateInput{
			CampaignID: campaignID, BackerID: backer, TierID: &tierID, Amount: 100000,
		})
		if err != nil {
			t.Fatalf("CreatePledge %d: %v", i+1, err)
		}
		pledges = append(pledges, p)
	}
	for i, p := range pledges {
		body := webhookBody(t, "evt_"+p.ID.String(), "payment.captured", fake.PaymentEntity{
			ID: "pay_" + p.ID.String(), OrderID: p.ProviderOrderID, Amount: 100000,
			Fee: 2000, Tax: 360, Status: "captured",
		})
		if err := svc.HandleWebhook(ctx, body); err != nil {
			t.Fatalf("capture %d must commit: %v", i+1, err)
		}
	}

	var status, reason string
	if err := pool.QueryRow(ctx, `SELECT status, COALESCE(failure_reason,'') FROM pledges WHERE id=$1`,
		pledges[1].ID).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "REFUND_PENDING" || reason != RefundReasonTierSoldOut {
		t.Fatalf("second pledge = %s/%q, want REFUND_PENDING/%s", status, reason, RefundReasonTierSoldOut)
	}

	var claimed int
	var raised int64
	if err := pool.QueryRow(ctx, `SELECT claimed_count FROM reward_tiers WHERE id=$1`, tierID).Scan(&claimed); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT raised_amount FROM campaigns WHERE id=$1`, campaignID).Scan(&raised); err != nil {
		t.Fatal(err)
	}
	if claimed != 1 || raised != 100000 {
		t.Fatalf("claimed=%d raised=%d, want 1 and 100000", claimed, raised)
	}

	var owed int64
	if err := pool.QueryRow(ctx, `
		SELECT -balance FROM ledger_balances WHERE kind='BACKER_REFUND_PAYABLE' AND owner_id=$1`,
		pledges[1].BackerID).Scan(&owed); err != nil {
		t.Fatalf("refund payable account: %v", err)
	}
	if owed != 100000 {
		t.Fatalf("owed to second backer = %d, want 100000", owed)
	}

	var platformAccounts int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM ledger_accounts WHERE kind='PLATFORM_ESCROW' AND owner_id IS NULL`).
		Scan(&platformAccounts); err != nil {
		t.Fatal(err)
	}
	if platformAccounts != 1 {
		t.Fatalf("PLATFORM_ESCROW accounts = %d, want exactly 1", platformAccounts)
	}
}

// Recording the same ledger reference twice must report ErrLedgerTxnExists
// without aborting the surrounding transaction.
func TestInsertLedgerTransaction_DuplicateLeavesTxUsable_Postgres(t *testing.T) {
	pool := capturePool(t)
	ctx := context.Background()
	repo := NewRepo(pool)
	ref := uuid.New()

	err := repo.Do(ctx, func(q Queries) error {
		txn := LedgerTxn{Kind: "ADJUSTMENT", ReferenceType: "pledge", ReferenceID: ref}
		if _, err := q.InsertLedgerTransaction(ctx, txn); err != nil {
			return err
		}
		if _, err := q.InsertLedgerTransaction(ctx, txn); !errors.Is(err, ErrLedgerTxnExists) {
			t.Fatalf("second insert = %v, want ErrLedgerTxnExists", err)
		}
		// The transaction must still accept statements.
		_, err := q.GetOrCreateAccount(ctx, KindPlatformEscrow, nil)
		return err
	})
	if err != nil {
		t.Fatalf("transaction aborted after a duplicate ledger reference: %v", err)
	}
}
