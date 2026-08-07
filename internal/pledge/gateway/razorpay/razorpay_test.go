package razorpay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maczeo11/cinefund/internal/pledge/gateway"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New("key_id", "key_secret", srv.URL)
}

func TestCreateOrder_SendsBasicAuthAndParses(t *testing.T) {
	var gotAuth string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/orders" {
			t.Errorf("path = %s, want /v1/orders", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"order_xyz","amount":50000,"receipt":"pl_1","status":"created"}`))
	})

	order, err := c.CreateOrder(context.Background(), gateway.OrderRequest{
		Amount: 50000, Currency: "INR", Receipt: "pl_1", PaymentCapture: 1,
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if order.ID != "order_xyz" {
		t.Errorf("id = %s, want order_xyz", order.ID)
	}
	if gotAuth == "" {
		t.Error("no Authorization header sent")
	}
}

func TestCreateOrder_EncodesRequestBody(t *testing.T) {
	var got map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"o","amount":100,"currency":"INR","receipt":"r","status":"created"}`))
	})

	_, err := c.CreateOrder(context.Background(), gateway.OrderRequest{
		Amount: 100, Currency: "INR", Receipt: "r", PaymentCapture: 1,
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if got["amount"] != float64(100) {
		t.Errorf("amount = %v, want 100", got["amount"])
	}
	if got["payment_capture"] != float64(1) {
		t.Errorf("payment_capture = %v, want 1", got["payment_capture"])
	}
}

func TestFetchPayments_ParsesItems(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/orders/order_1/payments" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"pay_1","order_id":"order_1","amount":100,"fee":2,"tax":0,"status":"captured"}]}`))
	})

	payments, err := c.FetchPayments(context.Background(), "order_1")
	if err != nil {
		t.Fatalf("FetchPayments: %v", err)
	}
	if len(payments) != 1 {
		t.Fatalf("len = %d, want 1", len(payments))
	}
	if payments[0].ID != "pay_1" || payments[0].Fee != 2 {
		t.Errorf("got %+v", payments[0])
	}
}

func TestCreateRefund(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/payments/pay_1/refund" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"rfd_1","payment_id":"pay_1","amount":100,"status":"processed"}`))
	})

	refund, err := c.CreateRefund(context.Background(), gateway.RefundRequest{
		PaymentID: "pay_1", Amount: 100, IdempotencyKey: "key",
	})
	if err != nil {
		t.Fatalf("CreateRefund: %v", err)
	}
	if refund.Status != "processed" {
		t.Errorf("status = %s, want processed", refund.Status)
	}
}

func TestProviderErrorIsReturned(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":"BAD_REQUEST_ERROR"}}`, http.StatusBadRequest)
	})
	_, err := c.CreateOrder(context.Background(), gateway.OrderRequest{})
	if err == nil {
		t.Fatal("expected an error from a 400 response")
	}
}
