// Package fake is a test double for the payment gateway.
package fake

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"

	"github.com/maczeo11/cinefund/internal/pledge/gateway"
)

// Fake is an in-memory gateway.
type Fake struct {
	mu     sync.Mutex
	orders map[string]*gateway.Order
	fails  map[string]error // method -> error to inject
}

// New returns an empty fake.
func New() *Fake {
	return &Fake{orders: make(map[string]*gateway.Order)}
}

// FailNext makes the next call to method return err.
func (f *Fake) FailNext(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fails == nil {
		f.fails = make(map[string]error)
	}
	f.fails[method] = err
}

func (f *Fake) takeFail(method string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.fails[method]; ok {
		delete(f.fails, method)
		return err
	}
	return nil
}

// CreateOrder records an order keyed by receipt.
func (f *Fake) CreateOrder(_ context.Context, req gateway.OrderRequest) (*gateway.Order, error) {
	if err := f.takeFail("CreateOrder"); err != nil {
		return nil, err
	}
	o := &gateway.Order{
		ID:      "order_test_" + req.Receipt,
		Amount:  req.Amount,
		Receipt: req.Receipt,
		Status:  "created",
	}
	f.mu.Lock()
	f.orders[o.ID] = o
	f.mu.Unlock()
	return o, nil
}

// FetchPayments returns the payments for an order.
func (f *Fake) FetchPayments(_ context.Context, orderID string) ([]gateway.Payment, error) {
	if err := f.takeFail("FetchPayments"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	o, ok := f.orders[orderID]
	f.mu.Unlock()
	if !ok {
		return nil, gateway.ErrOrderNotFound
	}
	return []gateway.Payment{{
		ID:      "pay_" + orderID,
		OrderID: orderID,
		Amount:  o.Amount,
		Status:  "captured",
	}}, nil
}

// CreateRefund returns a processed refund.
func (f *Fake) CreateRefund(_ context.Context, req gateway.RefundRequest) (*gateway.Refund, error) {
	if err := f.takeFail("CreateRefund"); err != nil {
		return nil, err
	}
	return &gateway.Refund{
		ID:        "refund_" + req.PaymentID,
		PaymentID: req.PaymentID,
		Amount:    req.Amount,
		Status:    "processed",
	}, nil
}

// PaymentEntity mirrors the razorpay payment.entity webhook payload.
type PaymentEntity struct {
	ID      string `json:"id"`
	OrderID string `json:"order_id"`
	Amount  int64  `json:"amount"`
	Fee     int64  `json:"fee"`
	Tax     int64  `json:"tax"`
	Status  string `json:"status"`
}

// Capture records a successful payment and returns the signed webhook body.
func (f *Fake) Capture(orderID string, entity PaymentEntity, secret string) (body []byte, sig string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if entity.OrderID == "" {
		entity.OrderID = orderID
	}
	evt := map[string]any{
		"id":    "evt_" + orderID,
		"event": "payment.captured",
		"payload": map[string]any{
			"payment": map[string]any{
				"entity": entity,
			},
		},
	}
	body, err = json.Marshal(evt)
	if err != nil {
		return nil, "", err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	sig = hex.EncodeToString(mac.Sum(nil))
	return body, sig, nil
}
