// Package pledge owns pledges, payments, the ledger, refunds and payouts.
//
// They live in one package because they are one transactional unit: you cannot
// capture a pledge without writing ledger entries, so splitting them across
// packages would mean exporting a transaction handle between packages.
//
// model.go has no imports of pgx, gin or the gateway SDK. If you cannot unit
// test the state machine with zero infrastructure, the layering has leaked.
package pledge

import (
	"time"

	"github.com/google/uuid"
)

// Status is the pledge lifecycle. The name is exactly the value stored in
// pledges.status, so a rename here is a migration and nothing less.
type Status string

const (
	StatusCreated       Status = "CREATED"        // row inserted, order created or pending
	StatusAuthorized    Status = "AUTHORIZED"     // payment.authorized seen
	StatusCaptured      Status = "CAPTURED"       // money held in escrow
	StatusFailed        Status = "FAILED"         // provider refused, or sweep gave up
	StatusRefundPending Status = "REFUND_PENDING" // refund requested from provider
	StatusRefunded      Status = "REFUNDED"       // money returned to backer
	StatusRefundFailed  Status = "REFUND_FAILED"  // provider refused to refund
	StatusSettled       Status = "SETTLED"        // campaign paid out
)

var terminal = map[Status]bool{
	StatusFailed:       true,
	StatusRefunded:     true,
	StatusSettled:      true,
	StatusRefundFailed: true,
}

// allowed holds the legal transitions. Forward-only; the table in docs/00 §5
// is the authority, this map is the code that enforces it.
//
// CREATED -> CAPTURED is legal because Razorpay in auto-capture mode collapses
// AUTHORIZED -> CAPTURED into a single payment.captured event (docs/00 §5.3).
// AUTHORIZED is still modelled for the manual-capture path.
var allowed = map[Status]map[Status]bool{
	StatusCreated: {
		StatusAuthorized:   true,
		StatusCaptured:     true, // auto-capture: payment.captured arrives directly
		StatusFailed:       true, // order creation failed / sweep timed out
		StatusRefundPending: true, // backer cancelled before capture (rule F9)
	},
	StatusAuthorized: {
		StatusCaptured: true,
		StatusFailed:   true, // capture failed / amount mismatch
	},
	StatusCaptured: {
		StatusRefundPending: true, // campaign failed / cancelled / backer cancel
		StatusSettled:       true, // payout paid
	},
	StatusRefundPending: {
		StatusRefunded: true,
		StatusRefundFailed: true,
	},
}

// CanTransitionTo reports whether from can move to to in one step.
func (from Status) CanTransitionTo(to Status) bool {
	if from == to {
		return true
	}
	return allowed[from][to]
}

// Terminal reports whether the pledge is in a state no future event changes.
func (from Status) Terminal() bool { return terminal[from] }

// Pledge is the domain aggregate.
type Pledge struct {
	ID                uuid.UUID
	CampaignID        uuid.UUID
	BackerID          uuid.UUID
	TierID            *uuid.UUID
	Amount            int64 // paise
	Currency          string
	Anonymous         bool
	Message           string
	Status            Status
	ProviderOrderID   string
	ProviderPaymentID string
	CapturedAt        *time.Time
	RefundedAt        *time.Time
	CreatedAt         time.Time
}

// Tier is the slice of the reward-tier row the pledge flow needs.
type Tier struct {
	ID            uuid.UUID
	CampaignID    uuid.UUID
	MinAmount     int64
	QuantityLimit *int
	ClaimedCount  int
}

// SoldOut reports whether the tier can accept another pledge. Only meaningful
// when QuantityLimit is set.
func (t *Tier) SoldOut() bool {
	return t.QuantityLimit != nil && t.ClaimedCount >= *t.QuantityLimit
}

// Campaign is the slice of the campaign row the pledge flow needs.
type Campaign struct {
	ID        uuid.UUID
	CreatorID uuid.UUID
	Status    string
	Deadline  time.Time
}
