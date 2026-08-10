package pledge

import (
	"time"

	"github.com/google/uuid"
)

// Status values match the pledges.status column.
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

// CREATED -> CAPTURED is legal because Razorpay auto-capture skips AUTHORIZED.
var allowed = map[Status]map[Status]bool{
	StatusCreated: {
		StatusAuthorized:    true,
		StatusCaptured:      true, // auto-capture: payment.captured arrives directly
		StatusFailed:        true, // order creation failed / sweep timed out
		StatusRefundPending: true, // backer cancelled before capture
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
		StatusRefunded:     true,
		StatusRefundFailed: true,
	},
}

// CanTransitionTo checks if this transition is legal.
func (from Status) CanTransitionTo(to Status) bool {
	if from == to {
		return true
	}
	return allowed[from][to]
}

// Terminal returns true if no further transitions are possible.
func (from Status) Terminal() bool { return terminal[from] }

// Pledge is the main aggregate.
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

// Tier — just the fields the pledge flow needs.
type Tier struct {
	ID            uuid.UUID
	CampaignID    uuid.UUID
	MinAmount     int64
	QuantityLimit *int
	ClaimedCount  int
}

// SoldOut returns true when the tier has hit its limit.
func (t *Tier) SoldOut() bool {
	return t.QuantityLimit != nil && t.ClaimedCount >= *t.QuantityLimit
}

// Campaign — just the fields the pledge flow reads.
type Campaign struct {
	ID        uuid.UUID
	CreatorID uuid.UUID
	Status    string
	Deadline  time.Time
}
