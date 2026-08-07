// Package razorpay is the real payment-provider adapter. It talks to the
// Razorpay REST API over HTTP; gateway/fake is the in-memory version tests use.
package razorpay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/maczeo11/cinefund/internal/pledge/gateway"
)

// Client is an HTTP client with a base URL. The base URL is swappable so tests
// can point it at a local fake server.
type Client struct {
	keyID     string
	keySecret string
	baseURL   string
	http      *http.Client
}

// New builds a client. baseURL defaults to the production Razorpay endpoint.
func New(keyID, keySecret string, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://api.razorpay.com"
	}
	return &Client{
		keyID:     keyID,
		keySecret: keySecret,
		baseURL:   baseURL,
		http:      &http.Client{Timeout: 30 * time.Second},
	}
}

// CreateOrder posts a new order. Razorpay returns 200 with the order object, or
// 4xx with an error object; either way the JSON shape is the same envelope.
func (c *Client) CreateOrder(ctx context.Context, req gateway.OrderRequest) (*gateway.Order, error) {
	body, err := json.Marshal(map[string]any{
		"amount":          req.Amount,
		"currency":        req.Currency,
		"receipt":         req.Receipt,
		"notes":           req.Notes,
		"payment_capture": req.PaymentCapture,
	})
	if err != nil {
		return nil, err
	}

	var out struct {
		ID      string `json:"id"`
		Amount  int64  `json:"amount"`
		Receipt string `json:"receipt"`
		Status  string `json:"status"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/orders", body, &out); err != nil {
		return nil, err
	}
	return &gateway.Order{
		ID:      out.ID,
		Amount:  out.Amount,
		Receipt: out.Receipt,
		Status:  out.Status,
	}, nil
}

// FetchPayments lists the payments attempted against an order. Razorpay returns
// them under {"items": [...]}.
func (c *Client) FetchPayments(ctx context.Context, orderID string) ([]gateway.Payment, error) {
	var out struct {
		Items []struct {
			ID      string `json:"id"`
			OrderID string `json:"order_id"`
			Amount  int64  `json:"amount"`
			Fee     int64  `json:"fee"`
			Tax     int64  `json:"tax"`
			Status  string `json:"status"`
		} `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/orders/"+orderID+"/payments", nil, &out); err != nil {
		return nil, err
	}
	payments := make([]gateway.Payment, 0, len(out.Items))
	for _, p := range out.Items {
		payments = append(payments, gateway.Payment{
			ID:      p.ID,
			OrderID: p.OrderID,
			Amount:  p.Amount,
			Fee:     p.Fee,
			Tax:     p.Tax,
			Status:  p.Status,
		})
	}
	return payments, nil
}

// CreateRefund issues a refund against a payment.
func (c *Client) CreateRefund(ctx context.Context, req gateway.RefundRequest) (*gateway.Refund, error) {
	body, err := json.Marshal(map[string]any{
		"amount":      req.Amount,
		"notes":       map[string]string{"idempotency_key": req.IdempotencyKey},
		"reverse_all": 0,
	})
	if err != nil {
		return nil, err
	}

	var out struct {
		ID        string `json:"id"`
		PaymentID string `json:"payment_id"`
		Amount    int64  `json:"amount"`
		Status    string `json:"status"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/payments/"+req.PaymentID+"/refund", body, &out); err != nil {
		return nil, err
	}
	return &gateway.Refund{
		ID:        out.ID,
		PaymentID: out.PaymentID,
		Amount:    out.Amount,
		Status:    out.Status,
	}, nil
}

// do performs one authenticated request. The Razorpay API uses HTTP Basic auth
// with the key id and secret.
func (c *Client) do(ctx context.Context, method, path string, body []byte, dst any) error {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, r)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(c.keyID, c.keySecret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("provider error %d: %s", resp.StatusCode, string(raw))
	}
	if dst == nil {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	return nil
}
