package pledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/maczeo11/cinefund/internal/platform/crypto"
	"github.com/maczeo11/cinefund/internal/platform/errs"
	"github.com/maczeo11/cinefund/internal/platform/postgres"
	"github.com/maczeo11/cinefund/internal/pledge/gateway"
)

var ErrDuplicateEvent = errors.New("duplicate event")

type Redis interface {
	SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error)
	Del(ctx context.Context, keys ...string) (int64, error)
}

type Repository interface {
	AttachOrder(ctx context.Context, pledgeID uuid.UUID, orderID string) error
}

type TxRunner interface {
	Do(ctx context.Context, fn func(q Queries) error) error
}

// Secrets are the two different Razorpay keys. Mixing them up is easy and the
// failure looks identical to a tampered payload, so they are named here rather
// than passed as bare strings.
type Secrets struct {
	KeySecret     string // signs the checkout callback
	WebhookSecret string // signs webhook bodies
}

type Service struct {
	repo    Repository
	tx      TxRunner
	ledger  *Ledger
	gw      gateway.Gateway
	redis   Redis
	log     *slog.Logger
	secrets Secrets
}

func NewService(
	repo Repository,
	tx TxRunner,
	ledger *Ledger,
	gw gateway.Gateway,
	redis Redis,
	secrets Secrets,
	log *slog.Logger,
) *Service {
	return &Service{repo: repo, tx: tx, ledger: ledger, gw: gw, redis: redis, secrets: secrets, log: log}
}

type CreateInput struct {
	CampaignID uuid.UUID
	BackerID   uuid.UUID
	TierID     *uuid.UUID
	Amount     int64
	Anonymous  bool
	Message    string
}

// CreatePledge inserts the pledge then creates the provider order.
func (s *Service) CreatePledge(ctx context.Context, in CreateInput) (*Pledge, error) {
	var pledge *Pledge
	err := s.tx.Do(ctx, func(q Queries) error {
		c, err := q.GetCampaignForUpdate(ctx, in.CampaignID)
		if err != nil {
			return err
		}
		if c.Status != "LIVE" {
			return errs.Conflict("CAMPAIGN_NOT_LIVE", "campaign is not live")
		}
		if time.Now().After(c.Deadline.Add(-60 * time.Second)) {
			return errs.Conflict("DEADLINE_PASSED", "pledges close 60s before the deadline")
		}
		if c.CreatorID == in.BackerID {
			return errs.Forbidden("CREATOR_CANNOT_PLEDGE", "the creator cannot pledge their own campaign")
		}

		var tier *Tier
		if in.TierID != nil {
			tier, err = q.GetTierForUpdate(ctx, *in.TierID)
			if err != nil {
				return err
			}
			if tier.CampaignID != in.CampaignID {
				return errs.Invalid("TIER_MISMATCH", "tier does not belong to this campaign")
			}
			if in.Amount < tier.MinAmount {
				return errs.Invalid("AMOUNT_BELOW_TIER", "amount is below the tier minimum")
			}
			if tier.SoldOut() {
				return errs.Conflict("TIER_SOLD_OUT", "tier is sold out")
			}
		}

		pledge = &Pledge{
			ID:         uuid.New(),
			CampaignID: in.CampaignID,
			BackerID:   in.BackerID,
			TierID:     in.TierID,
			Amount:     in.Amount,
			Currency:   "INR",
			Anonymous:  in.Anonymous,
			Message:    in.Message,
			Status:     StatusCreated,
		}
		return q.InsertPledge(ctx, pledge)
	})
	if err != nil {
		return nil, err
	}

	order, err := s.gw.CreateOrder(ctx, gateway.OrderRequest{
		Amount:         pledge.Amount,
		Currency:       "INR",
		Receipt:        pledge.ID.String(),
		Notes:          map[string]string{"pledge_id": pledge.ID.String(), "campaign_id": in.CampaignID.String()},
		PaymentCapture: 1,
	})
	if err != nil {
		s.log.Error("order creation failed; pledge left for reconciliation", "pledge_id", pledge.ID, "error", err)
		return nil, errs.Unavailable("PAYMENT_PROVIDER_UNAVAILABLE", "could not create payment order")
	}

	// webhook notes carry our pledge id so it'll still link up
	if err := s.repo.AttachOrder(ctx, pledge.ID, order.ID); err != nil {
		s.log.Error("order created but not attached", "pledge_id", pledge.ID, "order_id", order.ID, "error", err)
	}
	pledge.ProviderOrderID = order.ID
	return pledge, nil
}

// VerifySignature checks the HMAC-SHA256 signature.
func (s *Service) VerifySignature(raw []byte, signature string) error {
	return crypto.VerifyRazorpaySignature(raw, signature, s.secrets.WebhookSecret)
}

type ConfirmInput struct {
	PledgeID  uuid.UUID
	CallerID  uuid.UUID // must be the pledge's backer
	OrderID   string
	PaymentID string
	Signature string
}

// ConfirmPayment settles a pledge from the browser's checkout callback.
//
// Webhooks stay the source of truth, but they cannot reach a laptop and they
// can lag by minutes, so a backer who paid would otherwise watch the total sit
// still. Amounts are never taken from the request: only the order id is, and
// the figures are read back from the provider. Re-running this after the
// webhook has landed is a no-op, and vice versa.
func (s *Service) ConfirmPayment(ctx context.Context, in ConfirmInput) (Status, error) {
	if in.OrderID == "" {
		return "", errs.Invalid("MISSING_ORDER", "order_id is required")
	}
	// An empty key secret means the fake gateway is wired up, which has nothing
	// to sign with. Production config validation rejects an empty secret.
	if s.secrets.KeySecret != "" {
		if err := crypto.VerifyRazorpayCheckoutSignature(in.OrderID, in.PaymentID, in.Signature, s.secrets.KeySecret); err != nil {
			return "", errs.Unauthorized("BAD_SIGNATURE", "checkout signature verification failed")
		}
	}

	payments, err := s.gw.FetchPayments(ctx, in.OrderID)
	if err != nil {
		s.log.Error("could not fetch payments for confirm", "order_id", in.OrderID, "error", err)
		return "", errs.Unavailable("PAYMENT_PROVIDER_UNAVAILABLE", "could not read the payment status")
	}
	paid, captured := findCapture(payments, in.PaymentID)

	var status Status
	err = s.tx.Do(ctx, func(q Queries) error {
		pledge, err := q.GetPledgeForUpdate(ctx, in.PledgeID)
		if err != nil {
			if postgres.IsNoRows(err) {
				return errs.NotFound("PLEDGE_NOT_FOUND", "no such pledge")
			}
			return err
		}
		if pledge.BackerID != in.CallerID {
			return errs.Forbidden("NOT_YOUR_PLEDGE", "only the backer can confirm this pledge")
		}
		if pledge.ProviderOrderID != in.OrderID {
			return errs.Invalid("ORDER_MISMATCH", "order does not belong to this pledge")
		}
		if captured {
			if err := s.applyCapture(ctx, q, pledge, paid); err != nil {
				return err
			}
		}
		// Not captured yet means authorized or still pending; the webhook
		// finishes it. A capture held for refund reports REFUND_PENDING.
		status = pledge.Status
		return nil
	})
	if err != nil {
		return "", err
	}
	return status, nil
}

// RazorpayWebhookEvent is the subset of fields we care about from a webhook.
type RazorpayWebhookEvent struct {
	ID      string `json:"id"`
	Event   string `json:"event"`
	Payload struct {
		Payment struct {
			Entity struct {
				ID      string `json:"id"`
				OrderID string `json:"order_id"`
				Amount  int64  `json:"amount"`
				Fee     int64  `json:"fee"`
				Tax     int64  `json:"tax"`
				Status  string `json:"status"`
				Notes   struct {
					PledgeID string `json:"pledge_id"`
				} `json:"notes"`
			} `json:"entity"`
		} `json:"payment"`
	} `json:"payload"`
}

// HandleWebhook processes one verified webhook.
func (s *Service) HandleWebhook(ctx context.Context, raw []byte) error {
	var evt RazorpayWebhookEvent
	if err := json.Unmarshal(raw, &evt); err != nil {
		return errs.Invalid("MALFORMED_EVENT", "payload does not parse as a webhook event")
	}
	if evt.ID == "" {
		return errs.Invalid("MALFORMED_EVENT", "event has no id")
	}

	// Redis SETNX for fast dedup
	lockKey := "idem:wh:" + evt.ID
	acquired, err := s.redis.SetNX(ctx, lockKey, "1", 24*time.Hour)
	if err != nil {
		s.log.Warn("redis unavailable for idempotency; relying on postgres", "error", err)
	} else if !acquired {
		return ErrDuplicateEvent
	}

	err = s.tx.Do(ctx, func(q Queries) error {
		if err := q.InsertPaymentEvent(ctx, PaymentEvent{
			ID:              uuid.New(),
			Provider:        "razorpay",
			ProviderEventID: evt.ID,
			EventType:       evt.Event,
			PledgeID:        notedPledgeID(evt),
			Payload:         raw,
			SignatureValid:  true,
		}); err != nil {
			if postgres.IsUnique(err) {
				return ErrDuplicateEvent // uq_provider_event
			}
			return err
		}
		switch evt.Event {
		case "payment.captured":
			return s.captureFromWebhook(ctx, q, evt)
		case "payment.failed":
			return s.applyFailure(ctx, q, evt)
		default:
			return nil // recorded, unhandled
		}
	})

	// release the redis key on failure so retries aren't swallowed
	if err != nil && !errors.Is(err, ErrDuplicateEvent) {
		if _, delErr := s.redis.Del(ctx, lockKey); delErr != nil {
			s.log.Warn("failed to release webhook lock", "event_id", evt.ID, "error", delErr)
		}
	}
	return err
}

// capture is a payment reduced to the fields a capture needs, so the webhook
// and the checkout callback converge on one code path.
type capture struct {
	PaymentID string
	Amount    int64
	Fee       int64
	Tax       int64
}

func findCapture(payments []gateway.Payment, preferID string) (capture, bool) {
	var found *gateway.Payment
	for i, p := range payments {
		if p.Status != "captured" {
			continue
		}
		if p.ID == preferID {
			found = &payments[i]
			break
		}
		if found == nil {
			found = &payments[i]
		}
	}
	if found == nil {
		return capture{}, false
	}
	return capture{PaymentID: found.ID, Amount: found.Amount, Fee: found.Fee, Tax: found.Tax}, true
}

func (s *Service) captureFromWebhook(ctx context.Context, q Queries, evt RazorpayWebhookEvent) error {
	p := evt.Payload.Payment.Entity
	pledge, err := s.resolvePledge(ctx, q, evt.ID, p.OrderID, p.Notes.PledgeID)
	if err != nil {
		return err
	}
	return s.applyCapture(ctx, q, pledge, capture{
		PaymentID: p.ID,
		Amount:    p.Amount,
		Fee:       p.Fee,
		Tax:       p.Tax,
	})
}

// Refund reasons, stored in pledges.failure_reason when a capture is held for
// refund instead of counted toward the campaign.
const (
	RefundReasonTierSoldOut    = "TIER_SOLD_OUT"
	RefundReasonAmountMismatch = "AMOUNT_MISMATCH"
)

// applyCapture moves a pledge to CAPTURED and writes everything that hangs off
// that: the campaign total, the tier count, the ledger and the outbox row.
//
// By the time a capture arrives the backer's money has already moved, so a
// capture is never refused for a business reason. One that cannot count
// toward the campaign is held for refund instead (see holdForRefund).
// Returning an error would roll everything back, fail the webhook, and have
// the provider retry a capture that can never apply, with the payment left
// unrecorded.
func (s *Service) applyCapture(ctx context.Context, q Queries, pledge *Pledge, c capture) error {
	switch pledge.Status {
	case StatusCaptured, StatusSettled, StatusRefundPending, StatusRefunded, StatusRefundFailed:
		return nil // already applied (or held for refund) by a prior delivery; not an error
	}
	if !pledge.Status.CanTransitionTo(StatusCaptured) {
		return fmt.Errorf("illegal transition %s -> CAPTURED for pledge %s", pledge.Status, pledge.ID)
	}
	if c.Amount != pledge.Amount {
		return s.holdForRefund(ctx, q, pledge, c, RefundReasonAmountMismatch)
	}
	if pledge.TierID != nil {
		// CreatePledge checks the limit too, but claimed_count only moves on
		// capture, so several open checkouts can race for the last slot.
		// Checking under the row lock here keeps chk_tier_not_oversold from
		// ever firing on money that has already been taken.
		tier, err := q.GetTierForUpdate(ctx, *pledge.TierID)
		if err != nil {
			return err
		}
		if tier.SoldOut() {
			return s.holdForRefund(ctx, q, pledge, c, RefundReasonTierSoldOut)
		}
	}

	if err := q.MarkPledgeCaptured(ctx, pledge.ID, c.PaymentID, time.Now()); err != nil {
		return err
	}
	if err := q.IncrementCampaignRaised(ctx, pledge); err != nil {
		return err
	}
	if pledge.TierID != nil {
		if err := q.IncrementTierClaimed(ctx, *pledge.TierID); err != nil {
			return err
		}
	}
	pledge.Status = StatusCaptured
	pledge.ProviderPaymentID = c.PaymentID
	if err := s.ledger.RecordPledgeCapture(ctx, q, pledge, c.Fee+c.Tax); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{
		"pledge_id":   pledge.ID,
		"campaign_id": pledge.CampaignID,
		"backer_id":   pledge.BackerID,
		"amount":      pledge.Amount,
		"currency":    pledge.Currency,
	})
	return q.InsertOutbox(ctx, OutboxEvent{
		ID:            uuid.New(),
		Type:          "pledge.captured",
		Version:       1,
		AggregateType: "pledge",
		AggregateID:   pledge.ID,
		Payload:       payload,
	})
}

// holdForRefund records a capture that cannot count toward the campaign. The
// payment stays on the pledge and the money is booked as owed to the backer,
// while the campaign total and the tier count are left alone. The
// pledge.refund_required outbox event is what issues the refund.
//
// The pledge goes straight to REFUND_PENDING: logically CAPTURED then
// REFUND_PENDING, both legal, written as one update.
func (s *Service) holdForRefund(ctx context.Context, q Queries, pledge *Pledge, c capture, reason string) error {
	s.log.Warn("capture held for refund",
		"pledge_id", pledge.ID, "payment_id", c.PaymentID, "reason", reason,
		"paid", c.Amount, "pledged", pledge.Amount)

	now := time.Now()
	if err := q.MarkPledgeRefundPending(ctx, pledge.ID, c.PaymentID, now, reason); err != nil {
		return err
	}
	pledge.Status = StatusRefundPending
	pledge.ProviderPaymentID = c.PaymentID
	pledge.CapturedAt = &now
	pledge.FailureReason = reason
	if err := s.ledger.RecordRefundableCapture(ctx, q, pledge, c.Amount, c.Fee+c.Tax); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{
		"pledge_id":   pledge.ID,
		"campaign_id": pledge.CampaignID,
		"backer_id":   pledge.BackerID,
		"payment_id":  c.PaymentID,
		"amount":      c.Amount, // what was paid, which is what goes back
		"currency":    pledge.Currency,
		"reason":      reason,
	})
	return q.InsertOutbox(ctx, OutboxEvent{
		ID:            uuid.New(),
		Type:          "pledge.refund_required",
		Version:       1,
		AggregateType: "pledge",
		AggregateID:   pledge.ID,
		Payload:       payload,
	})
}

func (s *Service) applyFailure(ctx context.Context, q Queries, evt RazorpayWebhookEvent) error {
	p := evt.Payload.Payment.Entity
	pledge, err := s.resolvePledge(ctx, q, evt.ID, p.OrderID, p.Notes.PledgeID)
	if err != nil {
		return err
	}
	if pledge.Status == StatusFailed || pledge.Status.Terminal() {
		return nil
	}
	if !pledge.Status.CanTransitionTo(StatusFailed) {
		// A failed attempt reported after the order was already paid: webhooks
		// arrive out of order, and Checkout lets the backer retry on the same
		// order. The capture stands, so there is nothing to undo, and an error
		// would only make the provider retry this event forever.
		s.log.Info("ignoring stale payment failure", "pledge_id", pledge.ID, "status", pledge.Status)
		return nil
	}
	return q.SetPledgeStatus(ctx, pledge.ID, StatusFailed)
}

// resolvePledge finds the pledge an event belongs to. The order id is the
// normal route; the pledge id we stamped into the order notes covers the window
// where the order was created but AttachOrder had not landed yet.
func (s *Service) resolvePledge(ctx context.Context, q Queries, eventID, orderID, notedID string) (*Pledge, error) {
	if orderID == "" {
		return nil, fmt.Errorf("event %s has no order_id", eventID)
	}
	pledge, err := q.GetPledgeByOrderID(ctx, orderID)
	if err == nil {
		return pledge, nil
	}
	if !postgres.IsNoRows(err) || notedID == "" {
		return nil, err
	}
	id, perr := uuid.Parse(notedID)
	if perr != nil {
		return nil, fmt.Errorf("malformed pledge_id in notes: %w", perr)
	}
	return q.GetPledgeForUpdate(ctx, id)
}

func notedPledgeID(evt RazorpayWebhookEvent) *uuid.UUID {
	id, err := uuid.Parse(evt.Payload.Payment.Entity.Notes.PledgeID)
	if err != nil {
		return nil
	}
	return &id
}
