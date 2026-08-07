package pledge

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/maczeo11/cinefund/internal/platform/errs"
	"github.com/maczeo11/cinefund/internal/platform/httpx"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) CreatePledge(c *gin.Context) {
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Abort(c, errs.Invalid("INVALID_ID", "campaign id is not a uuid"))
		return
	}

	var body struct {
		BackerID  uuid.UUID  `json:"backer_id"`
		TierID    *uuid.UUID `json:"tier_id"`
		Amount    int64      `json:"amount"`
		Anonymous bool       `json:"anonymous"`
		Message   string     `json:"message"`
	}
	if !httpx.BindJSON(c, &body) {
		return
	}
	// TODO: validate currency is INR once we add multi-currency
	if body.Amount < 1 {
		httpx.Abort(c, errs.Invalid("INVALID_AMOUNT", "amount must be positive"))
		return
	}

	pledge, err := h.svc.CreatePledge(c.Request.Context(), CreateInput{
		CampaignID: campaignID,
		BackerID:   body.BackerID,
		TierID:     body.TierID,
		Amount:     body.Amount,
		Anonymous:  body.Anonymous,
		Message:    body.Message,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}
	httpx.Created(c, gin.H{
		"id":       pledge.ID,
		"status":   pledge.Status,
		"order_id": pledge.ProviderOrderID,
		"amount":   pledge.Amount,
		"currency": pledge.Currency,
	})
}

func (h *Handler) Webhook(c *gin.Context) {
	raw, err := c.GetRawData()
	if err != nil {
		_ = c.Error(errs.Invalid("READ_BODY", "could not read request body"))
		return
	}

	sig := c.GetHeader("X-Razorpay-Signature")
	if err := h.svc.VerifySignature(raw, sig); err != nil {
		_ = c.Error(errs.Unauthorized("BAD_SIGNATURE", "signature verification failed"))
		return
	}

	err = h.svc.HandleWebhook(c.Request.Context(), raw)
	if errors.Is(err, ErrDuplicateEvent) {
			httpx.OK(c, gin.H{"status": "ok"})
		return
	}
	if err != nil {
		_ = c.Error(err)
		return
	}
	httpx.OK(c, gin.H{"status": "ok"})
}
