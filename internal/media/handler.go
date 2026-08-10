package media

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/maczeo11/cinefund/internal/media/transcode"
	"github.com/maczeo11/cinefund/internal/platform/errs"
	"github.com/maczeo11/cinefund/internal/platform/httpx"
)

// Handler exposes the upload flow over HTTP.
type Handler struct {
	store   *UploadStore
	jobRepo *JobRepo
	ttl     time.Duration
}

func NewHandler(store *UploadStore, jobRepo *JobRepo) *Handler {
	return &Handler{store: store, jobRepo: jobRepo, ttl: 15 * time.Minute}
}

// Presign handles POST /uploads.
func (h *Handler) Presign(c *gin.Context) {
	var body struct {
		OwnerID     uuid.UUID  `json:"owner_id"`
		CampaignID  *uuid.UUID `json:"campaign_id"`
		Purpose     string     `json:"purpose"`
		ContentType string     `json:"content_type"`
	}
	if !httpx.BindJSON(c, &body) {
		return
	}
	purpose := transcode.Purpose(body.Purpose)
	if purpose == "" {
		purpose = transcode.PurposeFilm
	}

	asset, err := h.store.CreateAsset(c.Request.Context(), body.OwnerID, body.CampaignID, purpose, body.ContentType)
	if err != nil {
		_ = c.Error(err)
		return
	}
	url, err := h.store.PresignPut(c.Request.Context(), asset.StorageKey, h.ttl)
	if err != nil {
		_ = c.Error(err)
		return
	}
	httpx.Created(c, gin.H{
		"asset_id":   asset.ID,
		"upload_url": url,
	})
}

// Complete handles POST /uploads/:id/complete.
func (h *Handler) Complete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Abort(c, errs.Invalid("INVALID_ID", "asset id is not a uuid"))
		return
	}
	if err := h.store.MarkUploaded(c.Request.Context(), id); err != nil {
		if err == pgx.ErrNoRows {
			httpx.Abort(c, errs.NotFound("ASSET_NOT_FOUND", "asset not found"))
			return
		}
		_ = c.Error(err)
		return
	}
	// The transcode job is created via the outbox->Kafka consumer in normal
	// operation; for a single-node demo the queue is empty, so enqueue directly.
	if _, err := h.jobRepo.Enqueue(c.Request.Context(), id, 1); err != nil {
		_ = c.Error(err)
		return
	}
	httpx.OK(c, gin.H{"status": "queued"})
}
