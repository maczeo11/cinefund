package pledge

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
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
	MarkPledgeRefundPending(ctx context.Context, id uuid.UUID, paymentID string, at time.Time, reason string) error
	SetPledgeStatus(ctx context.Context, id uuid.UUID, s Status) error
	IncrementCampaignRaised(ctx context.Context, p *Pledge) error
	IncrementTierClaimed(ctx context.Context, tierID uuid.UUID) error
	InsertPaymentEvent(ctx context.Context, evt PaymentEvent) error
	// InsertLedgerTransaction returns ErrLedgerTxnExists when the reference
	// already has a transaction of this kind.
	InsertLedgerTransaction(ctx context.Context, t LedgerTxn) (uuid.UUID, error)
	InsertLedgerEntries(ctx context.Context, entries []LedgerEntry) error
	GetOrCreateAccount(ctx context.Context, kind AccountKind, ownerID *uuid.UUID) (uuid.UUID, error)
	InsertOutbox(ctx context.Context, evt OutboxEvent) error
}

type Ledger struct{}

func NewLedger() *Ledger { return &Ledger{} }

// ErrLedgerTxnExists means the transaction for this (kind, reference) is
// already on the books. uq_ledger_txn_reference lets each be recorded once.
var ErrLedgerTxnExists = errors.New("ledger transaction already recorded")

// RecordPledgeCapture books a capture that counts toward the campaign: the
// money moves into that campaign's escrow.
func (l *Ledger) RecordPledgeCapture(ctx context.Context, q Queries, p *Pledge, gatewayFee int64) error {
	return l.recordCapture(ctx, q, p, p.Amount, gatewayFee,
		KindCampaignEscrow, &p.CampaignID,
		fmt.Sprintf("capture %s", p.ProviderPaymentID))
}

// RecordRefundableCapture books a capture that is being handed back: the money
// is held for the backer instead of the campaign. paid is what the provider
// actually captured, which can differ from the pledged amount.
func (l *Ledger) RecordRefundableCapture(ctx context.Context, q Queries, p *Pledge, paid, gatewayFee int64) error {
	return l.recordCapture(ctx, q, p, paid, gatewayFee,
		KindBackerRefundPayable, &p.BackerID,
		fmt.Sprintf("capture %s held for refund: %s", p.ProviderPaymentID, p.FailureReason))
}

// recordCapture writes the double-entry rows for a capture of amount, credited
// to the given account.
func (l *Ledger) recordCapture(
	ctx context.Context, q Queries, p *Pledge, amount, gatewayFee int64,
	creditKind AccountKind, creditOwner *uuid.UUID, memo string,
) error {
	txnID, err := q.InsertLedgerTransaction(ctx, LedgerTxn{
		Kind:          "PLEDGE_CAPTURE",
		ReferenceType: "pledge",
		ReferenceID:   p.ID,
		Memo:          memo,
	})
	if errors.Is(err, ErrLedgerTxnExists) {
		return nil // already recorded; not an error
	}
	if err != nil {
		return err
	}

	escrow, err := q.GetOrCreateAccount(ctx, KindPlatformEscrow, nil)
	if err != nil {
		return err
	}
	credit, err := q.GetOrCreateAccount(ctx, creditKind, creditOwner)
	if err != nil {
		return err
	}

	entries := []LedgerEntry{
		{TransactionID: txnID, AccountID: escrow, Direction: "DEBIT", Amount: amount},
		{TransactionID: txnID, AccountID: credit, Direction: "CREDIT", Amount: amount},
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
