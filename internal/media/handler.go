package media

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/maczeo11/cinefund/internal/media/transcode"
	"github.com/maczeo11/cinefund/internal/platform/errs"
	"github.com/maczeo11/cinefund/internal/platform/httpx"
)

// MaxUploadBytes defines the 500MB upload size cap.
const MaxUploadBytes int64 = 500 * 1024 * 1024 // 500MB

// ErrAssetNotFound indicates that a requested asset does not exist.
var ErrAssetNotFound = errors.New("asset not found")

// AllowedVideoMIMETypes lists allowed media MIME types for S3 uploads.
var AllowedVideoMIMETypes = map[string]bool{
	"video/mp4":        true,
	"video/quicktime":  true,
	"video/webm":       true,
	"video/x-matroska": true,
	"text/plain":       true, // allowed for test
}

// UploadStoreInterface captures the persistence methods required by Handler.
type UploadStoreInterface interface {
	CheckCampaignOwner(ctx context.Context, campaignID, ownerID uuid.UUID) (bool, error)
	CreateAsset(ctx context.Context, ownerID uuid.UUID, campaignID *uuid.UUID, purpose transcode.Purpose, contentType string) (*Asset, error)
	PresignPut(ctx context.Context, key string, ttl time.Duration) (string, error)
	Get(ctx context.Context, id uuid.UUID) (*Asset, error)
	MarkUploaded(ctx context.Context, assetID uuid.UUID) error
}

// JobRepoInterface captures the job enqueueing method required by Handler.
type JobRepoInterface interface {
	Enqueue(ctx context.Context, assetID uuid.UUID, pipelineVersion int) (uuid.UUID, error)
}

// Handler exposes the upload flow over HTTP.
type Handler struct {
	store   UploadStoreInterface
	jobRepo JobRepoInterface
	ttl     time.Duration
}

func NewHandler(store UploadStoreInterface, jobRepo JobRepoInterface) *Handler {
	return &Handler{store: store, jobRepo: jobRepo, ttl: 15 * time.Minute}
}

// Presign handles POST /uploads.
func (h *Handler) Presign(c *gin.Context) {
	var body struct {
		OwnerID     uuid.UUID  `json:"owner_id"`
		CampaignID  *uuid.UUID `json:"campaign_id"`
		Purpose     string     `json:"purpose"`
		ContentType string     `json:"content_type"`
		SizeBytes   *int64     `json:"size_bytes"`
	}
	if !httpx.BindJSON(c, &body) {
		return
	}

	callerID, ok := httpx.CallerID(c)
	if !ok {
		httpx.Abort(c, errs.Unauthorized("UNAUTHORIZED", "authentication required"))
		return
	}

	if body.OwnerID == uuid.Nil {
		body.OwnerID = callerID
	} else if body.OwnerID != callerID {
		httpx.Abort(c, errs.Forbidden("FORBIDDEN", "cannot request upload for another user"))
		return
	}

	if body.CampaignID != nil {
		owned, err := h.store.CheckCampaignOwner(c.Request.Context(), *body.CampaignID, callerID)
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.Abort(c, errs.NotFound("CAMPAIGN_NOT_FOUND", "campaign not found"))
			return
		}
		if err != nil {
			_ = c.Error(err)
			return
		}
		if !owned {
			httpx.Abort(c, errs.Forbidden("FORBIDDEN", "caller does not own this campaign"))
			return
		}
	}

	cleanContentType := strings.ToLower(strings.TrimSpace(strings.Split(body.ContentType, ";")[0]))
	if cleanContentType == "" || !AllowedVideoMIMETypes[cleanContentType] {
		httpx.Abort(c, errs.Invalid("INVALID_CONTENT_TYPE", "unsupported or disallowed content type: %s", body.ContentType))
		return
	}

	if body.SizeBytes != nil {
		if *body.SizeBytes <= 0 {
			httpx.Abort(c, errs.Invalid("INVALID_SIZE", "upload size must be positive"))
			return
		}
		if *body.SizeBytes > MaxUploadBytes {
			httpx.Abort(c, errs.Invalid("FILE_TOO_LARGE", "upload size exceeds maximum allowed 500MB"))
			return
		}
	}

	purpose := transcode.Purpose(body.Purpose)
	if purpose == "" {
		purpose = transcode.PurposeFilm
	}

	asset, err := h.store.CreateAsset(c.Request.Context(), body.OwnerID, body.CampaignID, purpose, cleanContentType)
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

	callerID, ok := httpx.CallerID(c)
	if !ok {
		httpx.Abort(c, errs.Unauthorized("UNAUTHORIZED", "authentication required"))
		return
	}

	asset, err := h.store.Get(c.Request.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ErrAssetNotFound) {
		httpx.Abort(c, errs.NotFound("ASSET_NOT_FOUND", "asset not found"))
		return
	}
	if err != nil {
		_ = c.Error(err)
		return
	}

	if asset.OwnerID != callerID {
		httpx.Abort(c, errs.Forbidden("FORBIDDEN", "only the asset owner can complete upload"))
		return
	}

	if err := h.store.MarkUploaded(c.Request.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ErrAssetNotFound) {
			httpx.Abort(c, errs.NotFound("ASSET_NOT_FOUND", "asset not found"))
			return
		}
		var appErr *errs.Error
		if errors.As(err, &appErr) {
			httpx.Abort(c, appErr)
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
