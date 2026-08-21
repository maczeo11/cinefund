package pledge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maczeo11/cinefund/internal/pledge/gateway"
	"github.com/maczeo11/cinefund/internal/pledge/gateway/fake"
)

const testSecret = "test-webhook-secret"

func testService(fq *fakeQueries) (*Service, *fake.Fake, *fakeRedis) {
	gw := fake.New()
	rd := newFakeRedis()
	svc := NewService(fq, fq, NewLedger(), gw, rd, Secrets{WebhookSecret: testSecret},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	return svc, gw, rd
}
func liveCampaign(creator uuid.UUID, deadline time.Time) *Campaign {
	return &Campaign{ID: uuid.New(), CreatorID: creator, Status: "LIVE", Deadline: deadline}
}

func TestStatusCanTransitionTo(t *testing.T) {
	cases := []struct {
		name string
		from Status
		to   Status
		want bool
	}{
		{"created->authorized", StatusCreated, StatusAuthorized, true},
		{"created->captured", StatusCreated, StatusCaptured, true},
		{"created->failed", StatusCreated, StatusFailed, true},
		{"authorized->captured", StatusAuthorized, StatusCaptured, true},
		{"captured->refund_pending", StatusCaptured, StatusRefundPending, true},
		{"captured->settled", StatusCaptured, StatusSettled, true},
		{"refund_pending->refunded", StatusRefundPending, StatusRefunded, true},
		{"refunded->captured", StatusRefunded, StatusCaptured, false},
		{"captured->created", StatusCaptured, StatusCreated, false},
		{"same", StatusCaptured, StatusCaptured, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.from.CanTransitionTo(c.to); got != c.want {
				t.Fatalf("CanTransitionTo(%s,%s) = %v, want %v", c.from, c.to, got, c.want)
			}
		})
	}
}

func TestTerminal(t *testing.T) {
	if !StatusFailed.Terminal() || !StatusRefunded.Terminal() {
		t.Fatal("failed and refunded must be terminal")
	}
	if StatusCaptured.Terminal() {
		t.Fatal("captured is not terminal")
	}
}

func TestCreatePledge_RejectsWhenCampaignNotLive(t *testing.T) {
	fq := newFakeQueries()
	fq.seedCampaign(&Campaign{ID: uuid.New(), CreatorID: uuid.New(), Status: "DRAFT", Deadline: time.Now().Add(24 * time.Hour)})
	svc, _, _ := testService(fq)

	_, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: uuid.New(), BackerID: uuid.New(), Amount: 100000,
	})
	if err == nil {
		t.Fatal("expected an error for a DRAFT campaign")
	}
}

func TestCreatePledge_RejectsCreatorPledgingOwnCampaign(t *testing.T) {
	fq := newFakeQueries()
	creator := uuid.New()
	c := liveCampaign(creator, time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	svc, _, _ := testService(fq)

	_, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: creator, Amount: 100000,
	})
	if err == nil {
		t.Fatal("creator must not pledge their own campaign")
	}
}

func TestCreatePledge_RejectsAmountBelowTierMinimum(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	tier := &Tier{ID: uuid.New(), CampaignID: c.ID, MinAmount: 50000}
	fq.seedTier(tier)
	svc, _, _ := testService(fq)

	_, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: uuid.New(), TierID: &tier.ID, Amount: 10000,
	})
	if err == nil {
		t.Fatal("amount below tier minimum must be rejected")
	}
}

func TestCreatePledge_RejectsSoldOutTier(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	limit := 1
	tier := &Tier{ID: uuid.New(), CampaignID: c.ID, MinAmount: 10000, QuantityLimit: &limit, ClaimedCount: 1}
	fq.seedTier(tier)
	svc, _, _ := testService(fq)

	_, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: uuid.New(), TierID: &tier.ID, Amount: 100000,
	})
	if err == nil {
		t.Fatal("sold-out tier must be rejected")
	}
}

func TestCreatePledge_CreatesOrderAndAttaches(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	svc, _, _ := testService(fq)

	p, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: uuid.New(), Amount: 100000,
	})
	if err != nil {
		t.Fatalf("CreatePledge failed: %v", err)
	}
	if p.ProviderOrderID == "" {
		t.Fatal("expected a provider order id")
	}
	if p.Status != StatusCreated {
		t.Fatalf("pledge status = %s, want CREATED", p.Status)
	}
}

func captureBody(t *testing.T, gw *fake.Fake, orderID string, amount int64, secret string) ([]byte, string) {
	t.Helper()
	body, sig, err := gw.Capture(orderID, fake.PaymentEntity{
		ID:     "pay_test_1",
		Amount: amount,
		Fee:    2000,
		Tax:    500,
		Status: "captured",
	}, secret)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	return body, sig
}

// captured pledge should increment campaign, write ledger + outbox once
func TestHandleWebhook_CaptureHappyPath(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	backer := uuid.New()
	svc, gw, _ := testService(fq)

	p, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: backer, Amount: 100000,
	})
	if err != nil {
		t.Fatalf("CreatePledge: %v", err)
	}

	body, sig := captureBody(t, gw, p.ProviderOrderID, 100000, testSecret)
	if err := svc.VerifySignature(body, sig); err != nil {
		t.Fatalf("signature must verify: %v", err)
	}
	if err := svc.HandleWebhook(context.Background(), body); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}

	if got := fq.Pledge(p.ID).Status; got != StatusCaptured {
		t.Fatalf("status = %s, want CAPTURED", got)
	}
	if got := fq.Raised(c.ID); got != 100000 {
		t.Fatalf("raised = %d, want 100000", got)
	}
	if got := fq.OutboxLen(); got != 1 {
		t.Fatalf("outbox rows = %d, want 1", got)
	}
}

// 50 goroutines fire the same webhook, only one should win
func TestHandleWebhook_SameEvent50xConcurrent(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	svc, gw, _ := testService(fq)

	p, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: uuid.New(), Amount: 100000,
	})
	if err != nil {
		t.Fatalf("CreatePledge: %v", err)
	}

	body, _ := captureBody(t, gw, p.ProviderOrderID, 100000, testSecret)

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.HandleWebhook(context.Background(), body)
		}()
	}
	wg.Wait()
	close(errs)

	dups, failures := 0, 0
	for err := range errs {
		switch {
		case err == nil:
		case errors.Is(err, ErrDuplicateEvent):
			dups++
		default:
			failures++
			t.Errorf("unexpected error: %v", err)
		}
	}
	if failures != 0 {
		t.Fatalf("non-duplicate failures: %d", failures)
	}
	if got := fq.Raised(c.ID); got != 100000 {
		t.Fatalf("raised = %d, want exactly 100000", got)
	}
	if got := fq.Pledge(p.ID).Status; got != StatusCaptured {
		t.Fatalf("status = %s, want CAPTURED", got)
	}
	if dups != n-1 {
		t.Fatalf("duplicates reported = %d, want %d", dups, n-1)
	}
}

// if redis loses the key, postgres constraint still catches duplicates
func TestHandleWebhook_RedisFlushedStillOnce(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	svc, gw, rd := testService(fq)

	p, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: uuid.New(), Amount: 100000,
	})
	if err != nil {
		t.Fatalf("CreatePledge: %v", err)
	}

	body, _ := captureBody(t, gw, p.ProviderOrderID, 100000, testSecret)

	if err := svc.HandleWebhook(context.Background(), body); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	rd.Flush() // Redis loses the key
	if err := svc.HandleWebhook(context.Background(), body); err != nil {
		if !errors.Is(err, ErrDuplicateEvent) {
			t.Fatalf("second delivery: %v", err)
		}
	}
	if got := fq.Raised(c.ID); got != 100000 {
		t.Fatalf("raised = %d, want exactly 100000", got)
	}
	if got := fq.OutboxLen(); got != 1 {
		t.Fatalf("outbox rows = %d, want 1", got)
	}
}

// wrong amount should fail without changing anything
func TestHandleWebhook_AmountMismatch(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	svc, gw, _ := testService(fq)

	p, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: uuid.New(), Amount: 100000,
	})
	if err != nil {
		t.Fatalf("CreatePledge: %v", err)
	}

	body, _ := captureBody(t, gw, p.ProviderOrderID, 99999, testSecret) // 1 paisa off
	if err := svc.HandleWebhook(context.Background(), body); err == nil {
		t.Fatal("amount mismatch must be an error")
	}
	if got := fq.Pledge(p.ID).Status; got != StatusCreated {
		t.Fatalf("status = %s, want unchanged CREATED", got)
	}
	if got := fq.Raised(c.ID); got != 0 {
		t.Fatalf("raised = %d, want 0", got)
	}
}

// failed delivery should release redis key so retries arent swallowed
func TestHandleWebhook_ReleasesLockOnFailure(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	svc, gw, rd := testService(fq)

	// Create a pledge so the fake has a known order id, then fire an event for
	// an order that does not exist: parses as JSON, fails to process.
	pledge, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: uuid.New(), Amount: 100000,
	})
	if err != nil {
		t.Fatalf("CreatePledge: %v", err)
	}
	_ = pledge

	body, _ := captureBody(t, gw, "order_that_does_not_exist", 100000, testSecret)
	if err := svc.HandleWebhook(context.Background(), body); err == nil {
		t.Fatal("expected a processing failure")
	}
	rd.mu.Lock()
	_, stillHeld := rd.keys["idem:wh:"+eventID(t, body)]
	rd.mu.Unlock()
	if stillHeld {
		t.Fatal("lock must be released after a failed delivery so the provider retry is not swallowed")
	}
}

func eventID(t *testing.T, body []byte) string {
	t.Helper()
	var evt struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &evt); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return evt.ID
}

func TestFetchPaymentsUnknownOrder(t *testing.T) {
	gw := fake.New()
	if _, err := gw.FetchPayments(context.Background(), "order_missing"); !errors.Is(err, gateway.ErrOrderNotFound) {
		t.Fatalf("expected ErrOrderNotFound, got %v", err)
	}
}

func TestConfirmPayment_CapturesAndCreditsCampaign(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	svc, _, _ := testService(fq)

	p, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: uuid.New(), Amount: 100000,
	})
	if err != nil {
		t.Fatalf("CreatePledge failed: %v", err)
	}

	status, err := svc.ConfirmPayment(context.Background(), ConfirmInput{
		PledgeID: p.ID, OrderID: p.ProviderOrderID,
	})
	if err != nil {
		t.Fatalf("ConfirmPayment failed: %v", err)
	}
	if status != StatusCaptured {
		t.Fatalf("status = %s, want CAPTURED", status)
	}
	if got := fq.Raised(c.ID); got != 100000 {
		t.Fatalf("raised = %d, want 100000", got)
	}
	if got := fq.OutboxLen(); got != 1 {
		t.Fatalf("outbox rows = %d, want 1", got)
	}
}

func TestConfirmPayment_IsIdempotent(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	svc, _, _ := testService(fq)

	p, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: uuid.New(), Amount: 100000,
	})
	if err != nil {
		t.Fatalf("CreatePledge failed: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, err := svc.ConfirmPayment(context.Background(), ConfirmInput{
			PledgeID: p.ID, OrderID: p.ProviderOrderID,
		}); err != nil {
			t.Fatalf("confirm %d failed: %v", i, err)
		}
	}
	if got := fq.Raised(c.ID); got != 100000 {
		t.Fatalf("raised = %d after three confirms, want 100000", got)
	}
	if got := fq.Backers(c.ID); got != 1 {
		t.Fatalf("backers = %d, want 1", got)
	}
}

func TestConfirmPayment_RejectsOrderFromAnotherPledge(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	svc, _, _ := testService(fq)

	mine, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: uuid.New(), Amount: 100000,
	})
	if err != nil {
		t.Fatalf("CreatePledge failed: %v", err)
	}
	theirs, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: uuid.New(), Amount: 500000,
	})
	if err != nil {
		t.Fatalf("CreatePledge failed: %v", err)
	}

	if _, err := svc.ConfirmPayment(context.Background(), ConfirmInput{
		PledgeID: mine.ID, OrderID: theirs.ProviderOrderID,
	}); err == nil {
		t.Fatal("expected a mismatch error when confirming another pledge's order")
	}
	if got := fq.Raised(c.ID); got != 0 {
		t.Fatalf("raised = %d, want 0", got)
	}
}

func TestConfirmPayment_RejectsForgedCheckoutSignature(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	svc := NewService(fq, fq, NewLedger(), fake.New(), newFakeRedis(),
		Secrets{KeySecret: "live-key-secret", WebhookSecret: testSecret},
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	p, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: uuid.New(), Amount: 100000,
	})
	if err != nil {
		t.Fatalf("CreatePledge failed: %v", err)
	}

	if _, err := svc.ConfirmPayment(context.Background(), ConfirmInput{
		PledgeID: p.ID, OrderID: p.ProviderOrderID, PaymentID: "pay_x", Signature: "deadbeef",
	}); err == nil {
		t.Fatal("a forged checkout signature must be rejected")
	}
	if got := fq.Raised(c.ID); got != 0 {
		t.Fatalf("raised = %d, want 0", got)
	}
}
