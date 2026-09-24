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
	"github.com/maczeo11/cinefund/internal/platform/errs"
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
		{"failed->captured", StatusFailed, StatusCaptured, true},
		{"failed->refunded", StatusFailed, StatusRefunded, false},
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
	if !StatusRefunded.Terminal() || !StatusSettled.Terminal() {
		t.Fatal("refunded and settled must be terminal")
	}
	if StatusCaptured.Terminal() {
		t.Fatal("captured is not terminal")
	}
	// Checkout lets the backer retry on the same order after a failed attempt.
	if StatusFailed.Terminal() {
		t.Fatal("failed is not terminal: a retry on the same order can still capture")
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

// A capture for the wrong amount has still taken the backer's money. It is
// recorded and held for refund rather than failing the webhook forever.
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
	if err := svc.HandleWebhook(context.Background(), body); err != nil {
		t.Fatalf("a mismatched capture must be recorded, not failed: %v", err)
	}
	got := fq.Pledge(p.ID)
	if got.Status != StatusRefundPending || got.FailureReason != RefundReasonAmountMismatch {
		t.Fatalf("pledge = %s/%q, want REFUND_PENDING/%s", got.Status, got.FailureReason, RefundReasonAmountMismatch)
	}
	if got.ProviderPaymentID == "" {
		t.Fatal("the payment to refund must stay on the pledge")
	}
	if got := fq.Raised(c.ID); got != 0 {
		t.Fatalf("raised = %d, want 0", got)
	}
	// What was actually paid is what is owed back.
	if got := fq.Balance(KindBackerRefundPayable); got != 99999 {
		t.Fatalf("owed to backer = %d, want 99999", got)
	}
	if got := fq.Balance(KindCampaignEscrow); got != 0 {
		t.Fatalf("campaign escrow = %d, want 0", got)
	}
	if types := fq.OutboxTypes(); len(types) != 1 || types[0] != "pledge.refund_required" {
		t.Fatalf("outbox = %v, want [pledge.refund_required]", types)
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
		PledgeID: p.ID, CallerID: p.BackerID, OrderID: p.ProviderOrderID,
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
			PledgeID: p.ID, CallerID: p.BackerID, OrderID: p.ProviderOrderID,
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
		PledgeID: mine.ID, CallerID: mine.BackerID, OrderID: theirs.ProviderOrderID,
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
		PledgeID: p.ID, CallerID: p.BackerID, OrderID: p.ProviderOrderID, PaymentID: "pay_x", Signature: "deadbeef",
	}); err == nil {
		t.Fatal("a forged checkout signature must be rejected")
	}
	if got := fq.Raised(c.ID); got != 0 {
		t.Fatalf("raised = %d, want 0", got)
	}
}

func TestConfirmPayment_RejectsSomeoneElsesPledge(t *testing.T) {
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

	_, err = svc.ConfirmPayment(context.Background(), ConfirmInput{
		PledgeID: p.ID, CallerID: uuid.New(), OrderID: p.ProviderOrderID,
	})
	if errs.Code(err) != "NOT_YOUR_PLEDGE" {
		t.Fatalf("expected NOT_YOUR_PLEDGE, got %v", err)
	}
	if got := fq.Pledge(p.ID).Status; got != StatusCreated {
		t.Fatalf("status = %s, want unchanged CREATED", got)
	}
	if got := fq.Raised(c.ID); got != 0 {
		t.Fatalf("raised = %d, want 0", got)
	}
}

// webhookBody builds an unsigned webhook for HandleWebhook, which is only ever
// called after the handler has verified the signature.
func webhookBody(t *testing.T, eventID, event string, entity fake.PaymentEntity) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"id":      eventID,
		"event":   event,
		"payload": map[string]any{"payment": map[string]any{"entity": entity}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return body
}

// Two backers can both open checkout for the last slot of a limited tier,
// because claimed_count only moves on capture. Both pay. The second capture
// used to trip chk_tier_not_oversold, roll back, and fail the webhook forever
// with the backer's money unrecorded.
func TestHandleWebhook_TierSoldOutWhilePaying_HoldsForRefund(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	limit := 1
	tier := &Tier{ID: uuid.New(), CampaignID: c.ID, MinAmount: 10000, QuantityLimit: &limit}
	fq.seedTier(tier)
	svc, _, _ := testService(fq)

	first, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: uuid.New(), TierID: &tier.ID, Amount: 100000,
	})
	if err != nil {
		t.Fatalf("first CreatePledge: %v", err)
	}
	second, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: uuid.New(), TierID: &tier.ID, Amount: 100000,
	})
	if err != nil {
		t.Fatalf("second CreatePledge must pass: the slot is not claimed until capture: %v", err)
	}

	for i, p := range []*Pledge{first, second} {
		body := webhookBody(t, "evt_"+p.ProviderOrderID, "payment.captured", fake.PaymentEntity{
			ID: "pay_" + p.ProviderOrderID, OrderID: p.ProviderOrderID, Amount: 100000, Status: "captured",
		})
		if err := svc.HandleWebhook(context.Background(), body); err != nil {
			t.Fatalf("capture %d must not fail the webhook: %v", i+1, err)
		}
	}

	if got := fq.Pledge(first.ID).Status; got != StatusCaptured {
		t.Fatalf("first pledge = %s, want CAPTURED", got)
	}
	got := fq.Pledge(second.ID)
	if got.Status != StatusRefundPending || got.FailureReason != RefundReasonTierSoldOut {
		t.Fatalf("second pledge = %s/%q, want REFUND_PENDING/%s", got.Status, got.FailureReason, RefundReasonTierSoldOut)
	}
	if got := fq.TierClaimed(tier.ID); got != 1 {
		t.Fatalf("tier claimed = %d, want 1 (never oversold)", got)
	}
	if got := fq.Raised(c.ID); got != 100000 {
		t.Fatalf("raised = %d, want only the first pledge", got)
	}
	if got := fq.Backers(c.ID); got != 1 {
		t.Fatalf("backers = %d, want 1", got)
	}
	if got := fq.Balance(KindBackerRefundPayable); got != 100000 {
		t.Fatalf("owed to backer = %d, want 100000", got)
	}
	types := fq.OutboxTypes()
	if len(types) != 2 || types[0] != "pledge.captured" || types[1] != "pledge.refund_required" {
		t.Fatalf("outbox = %v, want [pledge.captured pledge.refund_required]", types)
	}
}

// The checkout callback and the webhook both report a held capture; whichever
// lands second is a no-op, and confirm tells the browser it is being refunded.
func TestConfirmPayment_SoldOutTierReportsRefundPending(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	limit := 1
	tier := &Tier{ID: uuid.New(), CampaignID: c.ID, MinAmount: 10000, QuantityLimit: &limit}
	fq.seedTier(tier)
	svc, gw, _ := testService(fq)

	p, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: uuid.New(), TierID: &tier.ID, Amount: 100000,
	})
	if err != nil {
		t.Fatalf("CreatePledge: %v", err)
	}
	tier.ClaimedCount = 1 // someone else's capture took the slot meanwhile

	status, err := svc.ConfirmPayment(context.Background(), ConfirmInput{
		PledgeID: p.ID, CallerID: p.BackerID, OrderID: p.ProviderOrderID,
	})
	if err != nil {
		t.Fatalf("ConfirmPayment: %v", err)
	}
	if status != StatusRefundPending {
		t.Fatalf("confirm status = %s, want REFUND_PENDING", status)
	}

	body, _ := captureBody(t, gw, p.ProviderOrderID, 100000, testSecret)
	if err := svc.HandleWebhook(context.Background(), body); err != nil {
		t.Fatalf("webhook after a held confirm must be a no-op, got %v", err)
	}
	if got := fq.Balance(KindBackerRefundPayable); got != 100000 {
		t.Fatalf("owed to backer = %d, want 100000 exactly once", got)
	}
	if got := fq.OutboxLen(); got != 1 {
		t.Fatalf("outbox rows = %d, want 1", got)
	}
}

// Checkout lets a backer retry on the same order after a declined attempt. The
// decline marks the pledge FAILED; the later capture used to be an illegal
// transition that failed the webhook forever with the money unrecorded.
func TestHandleWebhook_CaptureAfterFailedAttempt(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	svc, _, _ := testService(fq)

	p, err := svc.CreatePledge(context.Background(), CreateInput{
		CampaignID: c.ID, BackerID: uuid.New(), Amount: 100000,
	})
	if err != nil {
		t.Fatalf("CreatePledge: %v", err)
	}

	failed := webhookBody(t, "evt_fail_1", "payment.failed", fake.PaymentEntity{
		ID: "pay_declined", OrderID: p.ProviderOrderID, Amount: 100000, Status: "failed",
	})
	if err := svc.HandleWebhook(context.Background(), failed); err != nil {
		t.Fatalf("payment.failed: %v", err)
	}
	if got := fq.Pledge(p.ID).Status; got != StatusFailed {
		t.Fatalf("after decline = %s, want FAILED", got)
	}

	captured := webhookBody(t, "evt_capture_2", "payment.captured", fake.PaymentEntity{
		ID: "pay_retry", OrderID: p.ProviderOrderID, Amount: 100000, Status: "captured",
	})
	if err := svc.HandleWebhook(context.Background(), captured); err != nil {
		t.Fatalf("capture after a failed attempt must apply: %v", err)
	}
	if got := fq.Pledge(p.ID); got.Status != StatusCaptured || got.ProviderPaymentID != "pay_retry" {
		t.Fatalf("pledge = %s/%s, want CAPTURED/pay_retry", got.Status, got.ProviderPaymentID)
	}
	if got := fq.Raised(c.ID); got != 100000 {
		t.Fatalf("raised = %d, want 100000", got)
	}
}

// Webhooks arrive out of order: a decline reported after the capture must not
// fail the webhook (and so be retried forever) or undo the capture.
func TestHandleWebhook_StaleFailureAfterCaptureIsIgnored(t *testing.T) {
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
	if err := svc.HandleWebhook(context.Background(), body); err != nil {
		t.Fatalf("capture: %v", err)
	}

	stale := webhookBody(t, "evt_fail_late", "payment.failed", fake.PaymentEntity{
		ID: "pay_declined", OrderID: p.ProviderOrderID, Amount: 100000, Status: "failed",
	})
	if err := svc.HandleWebhook(context.Background(), stale); err != nil {
		t.Fatalf("a stale failure must be accepted, got %v", err)
	}
	if got := fq.Pledge(p.ID).Status; got != StatusCaptured {
		t.Fatalf("status = %s, want CAPTURED to stand", got)
	}
	if got := fq.Raised(c.ID); got != 100000 {
		t.Fatalf("raised = %d, want 100000", got)
	}
}
