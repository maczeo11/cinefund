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
type Service struct {
	repo          Repository
	tx            TxRunner
	ledger        *Ledger
	gw            gateway.Gateway
	redis         Redis
	log           *slog.Logger
	webhookSecret string
}

func NewService(
	repo Repository,
	tx TxRunner,
	ledger *Ledger,
	gw gateway.Gateway,
	redis Redis,
	webhookSecret string,
	log *slog.Logger,
) *Service {
	return &Service{repo: repo, tx: tx, ledger: ledger, gw: gw, redis: redis, webhookSecret: webhookSecret, log: log}
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
	return crypto.VerifyRazorpaySignature(raw, signature, s.webhookSecret)
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
			return s.applyCapture(ctx, q, evt)
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

func (s *Service) applyCapture(ctx context.Context, q Queries, evt RazorpayWebhookEvent) error {
	p := evt.Payload.Payment.Entity
	if p.OrderID == "" {
		return fmt.Errorf("capture event %s has no order_id", evt.ID)
	}

	pledge, err := q.GetPledgeByOrderID(ctx, p.OrderID)
	if err != nil {
		if postgres.IsNoRows(err) {
			// order wasn't attached — fall back to notes
			if pid := p.Notes.PledgeID; pid != "" {
				id, perr := uuid.Parse(pid)
				if perr != nil {
					return fmt.Errorf("malformed pledge_id in notes: %w", perr)
				}
				pledge, err = q.GetPledgeForUpdate(ctx, id)
			}
		}
		if err != nil {
			return err
		}
	}

	if pledge.Status == StatusCaptured || pledge.Status == StatusSettled {
		return nil // already applied by a prior delivery; not an error
	}
	if !pledge.Status.CanTransitionTo(StatusCaptured) {
		return fmt.Errorf("illegal transition %s -> CAPTURED for pledge %s", pledge.Status, pledge.ID)
	}
	if p.Amount != pledge.Amount {
		return fmt.Errorf("amount mismatch: paid %d, pledged %d", p.Amount, pledge.Amount)
	}

	if err := q.MarkPledgeCaptured(ctx, pledge.ID, p.ID, time.Now()); err != nil {
		return err
	}
	if err := q.IncrementCampaignRaised(ctx, pledge.CampaignID, pledge.Amount); err != nil {
		return err
	}
	if pledge.TierID != nil {
		if err := q.IncrementTierClaimed(ctx, *pledge.TierID); err != nil {
			return err
		}
	}
	if err := s.ledger.RecordPledgeCapture(ctx, q, pledge, p.Fee+p.Tax); err != nil {
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

func (s *Service) applyFailure(ctx context.Context, q Queries, evt RazorpayWebhookEvent) error {
	p := evt.Payload.Payment.Entity
	if p.OrderID == "" {
		return fmt.Errorf("failure event %s has no order_id", evt.ID)
	}
	pledge, err := q.GetPledgeByOrderID(ctx, p.OrderID)
	if err != nil {
		if postgres.IsNoRows(err) && p.Notes.PledgeID != "" {
			id, perr := uuid.Parse(p.Notes.PledgeID)
			if perr != nil {
				return fmt.Errorf("malformed pledge_id in notes: %w", perr)
			}
			pledge, err = q.GetPledgeForUpdate(ctx, id)
		}
		if err != nil {
			return err
		}
	}
	if pledge.Status == StatusFailed || pledge.Status.Terminal() {
		return nil
	}
	if !pledge.Status.CanTransitionTo(StatusFailed) {
		return fmt.Errorf("illegal transition %s -> FAILED for pledge %s", pledge.Status, pledge.ID)
	}
	return q.SetPledgeStatus(ctx, pledge.ID, StatusFailed)
}
