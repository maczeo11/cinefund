package campaign

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/maczeo11/cinefund/internal/platform/errs"
	"github.com/maczeo11/cinefund/internal/platform/httpx"
)

// CampaignStore defines the persistence interface required by Handler.
type CampaignStore interface {
	Create(ctx context.Context, in NewCampaign) (*Campaign, error)
	Get(ctx context.Context, id uuid.UUID) (*Campaign, error)
	List(ctx context.Context) ([]Campaign, error)
	SetLive(ctx context.Context, id uuid.UUID) error
	AddTier(ctx context.Context, in NewTier) (*Tier, error)
	Tiers(ctx context.Context, campaignID uuid.UUID) ([]Tier, error)
}

type Handler struct {
	store CampaignStore
}

func NewHandler(store CampaignStore) *Handler { return &Handler{store: store} }

func (h *Handler) List(c *gin.Context) {
	campaigns, err := h.store.List(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	if campaigns == nil {
		campaigns = []Campaign{}
	}
	httpx.OK(c, campaigns)
}

func (h *Handler) Create(c *gin.Context) {
	var body NewCampaign
	if !httpx.BindJSON(c, &body) {
		return
	}
	callerID, ok := httpx.CallerID(c)
	if !ok {
		httpx.Abort(c, errs.Unauthorized("UNAUTHORIZED", "authentication required"))
		return
	}
	if body.CreatorID == uuid.Nil {
		body.CreatorID = callerID
	} else if body.CreatorID != callerID {
		httpx.Abort(c, errs.Forbidden("FORBIDDEN", "cannot create campaign for another creator"))
		return
	}
	campaign, err := h.store.Create(c.Request.Context(), body)
	if err != nil {
		_ = c.Error(err)
		return
	}
	httpx.Created(c, campaign)
}

func (h *Handler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Abort(c, errs.Invalid("INVALID_ID", "campaign id is not a uuid"))
		return
	}
	campaign, err := h.store.Get(c.Request.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ErrNotFound) {
		httpx.Abort(c, errs.NotFound("CAMPAIGN_NOT_FOUND", "campaign not found"))
		return
	}
	if err != nil {
		_ = c.Error(err)
		return
	}
	httpx.OK(c, campaign)
}

func (h *Handler) SetLive(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Abort(c, errs.Invalid("INVALID_ID", "campaign id is not a uuid"))
		return
	}
	callerID, ok := httpx.CallerID(c)
	if !ok {
		httpx.Abort(c, errs.Unauthorized("UNAUTHORIZED", "authentication required"))
		return
	}
	campaign, err := h.store.Get(c.Request.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ErrNotFound) {
		httpx.Abort(c, errs.NotFound("CAMPAIGN_NOT_FOUND", "campaign not found"))
		return
	}
	if err != nil {
		_ = c.Error(err)
		return
	}
	if campaign.CreatorID != callerID {
		httpx.Abort(c, errs.Forbidden("FORBIDDEN", "caller does not own this campaign"))
		return
	}
	if err := h.store.SetLive(c.Request.Context(), id); err != nil {
		_ = c.Error(err)
		return
	}
	httpx.OK(c, gin.H{"id": id, "status": "LIVE"})
}

func (h *Handler) AddTier(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Abort(c, errs.Invalid("INVALID_ID", "campaign id is not a uuid"))
		return
	}
	callerID, ok := httpx.CallerID(c)
	if !ok {
		httpx.Abort(c, errs.Unauthorized("UNAUTHORIZED", "authentication required"))
		return
	}
	var body NewTier
	if !httpx.BindJSON(c, &body) {
		return
	}
	campaign, err := h.store.Get(c.Request.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ErrNotFound) {
		httpx.Abort(c, errs.NotFound("CAMPAIGN_NOT_FOUND", "campaign not found"))
		return
	}
	if err != nil {
		_ = c.Error(err)
		return
	}
	if campaign.CreatorID != callerID {
		httpx.Abort(c, errs.Forbidden("FORBIDDEN", "caller does not own this campaign"))
		return
	}
	body.CampaignID = id
	tier, err := h.store.AddTier(c.Request.Context(), body)
	if err != nil {
		_ = c.Error(err)
		return
	}
	httpx.Created(c, tier)
}

func (h *Handler) Tiers(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Abort(c, errs.Invalid("INVALID_ID", "campaign id is not a uuid"))
		return
	}
	tiers, err := h.store.Tiers(c.Request.Context(), id)
	if err != nil {
		_ = c.Error(err)
		return
	}
	if tiers == nil {
		tiers = []Tier{}
	}
	httpx.OK(c, tiers)
}
