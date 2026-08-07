// Package gateway is the payment provider abstraction.
//
// Three methods, no more. It is the smallest thing that could work: CineFund
// creates orders, polls payments (reconciliation), and creates refunds. Nothing
// about HTTP, HMAC, or Razorpay's SDK leaks past this interface, which is what
// lets every payment test run against the deterministic fake in gateway/fake
// with zero network access.
package gateway

import (
	"context"
	"errors"
)

// ErrOrderNotFound is returned by FetchPayments when the provider has never
// seen the order - the reconciliation sweep's signal to fail a stale pledge.
var ErrOrderNotFound = errors.New("order not found")

// OrderRequest is what the service sends to open a checkout order.
type OrderRequest struct {
	Amount   int64             // paise, same unit as our BIGINT - never multiply
	Currency string            // "INR"
	Receipt  string            // our pledge id, so a webhook can find its way home
	Notes    map[string]string // pledge_id, campaign_id - lets a webhook recover an unattached pledge

	// PaymentCapture = 1 means auto-capture: Razorpay settles the payment at
	// authorisation time and sends payment.captured, which is the event
	// applyCapture listens for.
	PaymentCapture int
}

// Order is a provider order. ID is the provider's order id, stored on
// pledges.provider_order_id.
type Order struct {
	ID      string
	Amount  int64
	Receipt string
	Status  string
}

// Payment is one payment attempt against an order. An order can have several
// (card declined, retry); at most one can succeed.
type Payment struct {
	ID      string
	OrderID string
	Amount  int64
	Fee     int64
	Tax     int64
	Status  string
}

// RefundRequest asks the provider to refund a payment. IdempotencyKey is a
// deterministic value (derived from our refund id) the provider dedupes on.
type RefundRequest struct {
	PaymentID      string
	Amount         int64
	IdempotencyKey string
}

// Refund is a provider refund.
type Refund struct {
	ID        string
	PaymentID string
	Amount    int64
	Status    string
}

// Gateway is implemented by the Razorpay adapter and the deterministic fake.
type Gateway interface {
	CreateOrder(ctx context.Context, req OrderRequest) (*Order, error)
	FetchPayments(ctx context.Context, orderID string) ([]Payment, error)
	CreateRefund(ctx context.Context, req RefundRequest) (*Refund, error)
}
