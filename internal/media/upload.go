package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/maczeo11/cinefund/internal/media/transcode"
	"github.com/maczeo11/cinefund/internal/platform/errs"
	"github.com/maczeo11/cinefund/internal/platform/objectstore"
)

// Asset is the subset of media_assets the upload flow touches.
type Asset struct {
	ID          uuid.UUID
	OwnerID     uuid.UUID
	CampaignID  *uuid.UUID
	Purpose     transcode.Purpose
	StorageKey  string
	ContentType string
	Status      string
	SizeBytes   int64
}

// UploadStore owns media_assets rows and hands out presigned URLs.
type UploadStore struct {
	pool *pgxpool.Pool
	obj  *objectstore.Store
}

func NewUploadStore(pool *pgxpool.Pool, obj *objectstore.Store) *UploadStore {
	return &UploadStore{pool: pool, obj: obj}
}

// CreateAsset inserts a PENDING_UPLOAD row.
func (s *UploadStore) CreateAsset(ctx context.Context, ownerID uuid.UUID, campaignID *uuid.UUID, purpose transcode.Purpose, contentType string) (*Asset, error) {
	id := uuid.New()
	key := fmt.Sprintf("uploads/%s/%s", ownerID, id.String())
	_, err := s.pool.Exec(ctx, `
		INSERT INTO media_assets (id, owner_id, campaign_id, purpose, storage_key, content_type, status)
		VALUES ($1,$2,$3,$4,$5,$6,'PENDING_UPLOAD')`,
		id, ownerID, campaignID, string(purpose), key, contentType)
	if err != nil {
		return nil, err
	}
	return &Asset{ID: id, OwnerID: ownerID, CampaignID: campaignID, Purpose: purpose, StorageKey: key, ContentType: contentType, Status: "PENDING_UPLOAD"}, nil
}

// Get returns one asset row.
func (s *UploadStore) Get(ctx context.Context, id uuid.UUID) (*Asset, error) {
	var a Asset
	var campaignID *uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT id, owner_id, campaign_id, purpose, storage_key, content_type, status, COALESCE(size_bytes,0)
		  FROM media_assets WHERE id = $1`, id).
		Scan(&a.ID, &a.OwnerID, &campaignID, &a.Purpose, &a.StorageKey, &a.ContentType, &a.Status, &a.SizeBytes)
	if err != nil {
		return nil, err
	}
	a.CampaignID = campaignID
	return &a, nil
}

// CheckCampaignOwner returns true if the campaign exists and its creator_id matches ownerID.
func (s *UploadStore) CheckCampaignOwner(ctx context.Context, campaignID, ownerID uuid.UUID) (bool, error) {
	if s.pool == nil {
		return true, nil
	}
	var creatorID uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT creator_id FROM campaigns WHERE id = $1`, campaignID).Scan(&creatorID)
	if err != nil {
		return false, err
	}
	return creatorID == ownerID, nil
}

// PresignPut signs a browser PUT for the asset's storage key.
func (s *UploadStore) PresignPut(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return s.obj.PresignedPut(ctx, key, ttl)
}

// PlaybackAsset is the subset of media_assets the playback flow touches.
type PlaybackAsset struct {
	ID              uuid.UUID
	Status          string
	MasterKey       string
	PipelineVersion int
	Rungs           []PlaybackRung
}

// PlaybackRung is one decoded entry of the renditions JSONB column.
type PlaybackRung struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// GetPlaybackAsset returns one asset with its rendition list for playback.
func (s *UploadStore) GetPlaybackAsset(ctx context.Context, id uuid.UUID) (*PlaybackAsset, error) {
	var a PlaybackAsset
	var masterKey *string
	var renditions []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, status, master_key, pipeline_version, COALESCE(renditions, '[]'::jsonb)
		  FROM media_assets WHERE id = $1`, id).
		Scan(&a.ID, &a.Status, &masterKey, &a.PipelineVersion, &renditions)
	if err != nil {
		return nil, err
	}
	if masterKey != nil {
		a.MasterKey = *masterKey
	}
	if err := json.Unmarshal(renditions, &a.Rungs); err != nil {
		return nil, fmt.Errorf("decode renditions for asset %s: %w", id, err)
	}
	return &a, nil
}

// LatestReadyAssetByCampaign returns the newest READY asset for a campaign,
// or (nil, nil) when the campaign has no watchable reel yet.
func (s *UploadStore) LatestReadyAssetByCampaign(ctx context.Context, campaignID uuid.UUID) (*PlaybackAsset, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM media_assets
		 WHERE campaign_id = $1 AND status = 'READY'
		 ORDER BY created_at DESC LIMIT 1`, campaignID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GetPlaybackAsset(ctx, id)
}

// GetObject fetches a private object (used for playlists, never segments).
func (s *UploadStore) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	if s.obj == nil {
		return nil, fmt.Errorf("object store not configured")
	}
	return s.obj.Get(ctx, key)
}

// PresignPublicGet signs a browser-reachable GET for a private object.
func (s *UploadStore) PresignPublicGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if s.obj == nil {
		return "", fmt.Errorf("object store not configured")
	}
	return s.obj.PresignedGetPublic(ctx, key, ttl)
}

// MarkUploaded verifies the object exists and flips the row to UPLOADED.
func (s *UploadStore) MarkUploaded(ctx context.Context, assetID uuid.UUID) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		var key string
		err := tx.QueryRow(ctx, `
			SELECT storage_key FROM media_assets WHERE id = $1 FOR UPDATE`, assetID).Scan(&key)
		if err != nil {
			return err
		}
		var sizeBytes int64
		if s.obj != nil {
			sz, _, err := s.obj.Stat(ctx, key)
			if err != nil {
				return fmt.Errorf("stat uploaded object: %w", err)
			}
			if sz > MaxUploadBytes {
				return errs.Invalid("FILE_TOO_LARGE", "uploaded object size exceeds maximum 500MB")
			}
			sizeBytes = sz
		}
		if sizeBytes > 0 {
			_, err = tx.Exec(ctx, `
				UPDATE media_assets SET status = 'UPLOADED', size_bytes = $2 WHERE id = $1`, assetID, sizeBytes)
		} else {
			_, err = tx.Exec(ctx, `
				UPDATE media_assets SET status = 'UPLOADED' WHERE id = $1`, assetID)
		}
		if err != nil {
			return err
		}
		return insertOutbox(ctx, tx, "media.uploaded", assetID, map[string]any{
			"asset_id": assetID,
		})
	})
}

// inTx runs fn in one transaction.
func (s *UploadStore) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}
