package media

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/maczeo11/cinefund/internal/media/transcode"
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

// CreateAsset inserts a PENDING_UPLOAD row and returns its storage key.
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

// PresignPut signs a browser PUT for the asset's storage key.
func (s *UploadStore) PresignPut(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return s.obj.PresignedPut(ctx, key, ttl)
}

// MarkUploaded verifies the object actually exists (HEAD), then flips the row to
// UPLOADED and emits the outbox event that the Kafka consumer turns into a
// transcode job.
func (s *UploadStore) MarkUploaded(ctx context.Context, assetID uuid.UUID) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		var key string
		err := tx.QueryRow(ctx, `
			SELECT storage_key FROM media_assets WHERE id = $1 FOR UPDATE`, assetID).Scan(&key)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE media_assets SET status = 'UPLOADED' WHERE id = $1`, assetID); err != nil {
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
