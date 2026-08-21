// Package gateway is the payment provider abstraction.
package gateway

import (
	"context"
	"errors"
)

// ErrOrderNotFound is returned when the provider has never seen the order.
var ErrOrderNotFound = errors.New("order not found")

// OrderRequest is what the service sends to open a checkout order.
type OrderRequest struct {
	Amount         int64
	Currency       string
	Receipt        string
	Notes          map[string]string
	PaymentCapture int
}

// Order is a provider order.
type Order struct {
	ID      string
	Amount  int64
	Receipt string
	Status  string
}

// Payment is one payment attempt against an order.
type Payment struct {
	ID      string
	OrderID string
	Amount  int64
	Fee     int64
	Tax     int64
	Status  string
}

// RefundRequest asks the provider to refund a payment.
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
