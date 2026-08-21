package campaign

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/maczeo11/cinefund/internal/platform/errs"
	"github.com/maczeo11/cinefund/internal/platform/httpx"
)

type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler { return &Handler{store: store} }

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
	if errors.Is(err, pgx.ErrNoRows) {
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
	var body NewTier
	if !httpx.BindJSON(c, &body) {
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
