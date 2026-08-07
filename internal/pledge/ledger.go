package pledge

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/maczeo11/cinefund/internal/platform/postgres"
)

// AccountKind matches the check constraint in migrations/0011.
type AccountKind string

const (
	KindPlatformEscrow      AccountKind = "PLATFORM_ESCROW"
	KindCampaignEscrow      AccountKind = "CAMPAIGN_ESCROW"
	KindCreatorPayable      AccountKind = "CREATOR_PAYABLE"
	KindBackerRefundPayable AccountKind = "BACKER_REFUND_PAYABLE"
	KindPlatformFeeRevenue  AccountKind = "PLATFORM_FEE_REVENUE"
	KindGatewayFeeExpense   AccountKind = "GATEWAY_FEE_EXPENSE"
)

type LedgerTxn struct {
	ID            uuid.UUID
	Kind          string // PLEDGE_CAPTURE | REFUND | PAYOUT | FEE | ADJUSTMENT
	ReferenceType string // pledge | refund | payout
	ReferenceID   uuid.UUID
	Memo          string
	CreatedAt     time.Time
}

type LedgerEntry struct {
	ID            int64
	TransactionID uuid.UUID
	AccountID     uuid.UUID
	Direction     string // DEBIT | CREDIT
	Amount        int64
}

// Queries is tx-scoped so ledger writes are in the same tx as domain writes.
type Queries interface {
	InsertPledge(ctx context.Context, p *Pledge) error
	GetPledgeForUpdate(ctx context.Context, id uuid.UUID) (*Pledge, error)
	GetPledgeByOrderID(ctx context.Context, orderID string) (*Pledge, error)
	GetCampaignForUpdate(ctx context.Context, id uuid.UUID) (*Campaign, error)
	GetTierForUpdate(ctx context.Context, id uuid.UUID) (*Tier, error)
	MarkPledgeCaptured(ctx context.Context, id uuid.UUID, paymentID string, at time.Time) error
	SetPledgeStatus(ctx context.Context, id uuid.UUID, s Status) error
	IncrementCampaignRaised(ctx context.Context, campaignID uuid.UUID, amount int64) error
	IncrementTierClaimed(ctx context.Context, tierID uuid.UUID) error
	InsertPaymentEvent(ctx context.Context, evt PaymentEvent) error
	InsertLedgerTransaction(ctx context.Context, t LedgerTxn) (uuid.UUID, error)
	InsertLedgerEntries(ctx context.Context, entries []LedgerEntry) error
	GetOrCreateAccount(ctx context.Context, kind AccountKind, ownerID *uuid.UUID) (uuid.UUID, error)
	InsertOutbox(ctx context.Context, evt OutboxEvent) error
}

type Ledger struct{}

func NewLedger() *Ledger { return &Ledger{} }

// RecordPledgeCapture writes the double-entry ledger rows for a capture.
// Idempotent via uq_ledger_txn_reference.
func (l *Ledger) RecordPledgeCapture(ctx context.Context, q Queries, p *Pledge, gatewayFee int64) error {
	txnID, err := q.InsertLedgerTransaction(ctx, LedgerTxn{
		Kind:          "PLEDGE_CAPTURE",
		ReferenceType: "pledge",
		ReferenceID:   p.ID,
		Memo:          fmt.Sprintf("capture %s", p.ProviderPaymentID),
	})
	if err != nil {
		if postgres.IsUnique(err) {
			return nil // already recorded; not an error
		}
		return err
	}

	escrow, err := q.GetOrCreateAccount(ctx, KindPlatformEscrow, nil)
	if err != nil {
		return err
	}
	campEsc, err := q.GetOrCreateAccount(ctx, KindCampaignEscrow, &p.CampaignID)
	if err != nil {
		return err
	}

	entries := []LedgerEntry{
		{TransactionID: txnID, AccountID: escrow, Direction: "DEBIT", Amount: p.Amount},
		{TransactionID: txnID, AccountID: campEsc, Direction: "CREDIT", Amount: p.Amount},
	}
	if gatewayFee > 0 {
		feeExpense, err := q.GetOrCreateAccount(ctx, KindGatewayFeeExpense, nil)
		if err != nil {
			return err
		}
		entries = append(entries,
			LedgerEntry{TransactionID: txnID, AccountID: feeExpense, Direction: "DEBIT", Amount: gatewayFee},
			LedgerEntry{TransactionID: txnID, AccountID: escrow, Direction: "CREDIT", Amount: gatewayFee},
		)
	}
	return q.InsertLedgerEntries(ctx, entries)
}
