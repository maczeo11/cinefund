// Package media owns media assets and transcode jobs.
package media

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/maczeo11/cinefund/internal/media/transcode"
)

// JobRepo implements transcode.JobStore against Postgres.
type JobRepo struct{ pool *pgxpool.Pool }

func NewJobRepo(pool *pgxpool.Pool) *JobRepo { return &JobRepo{pool: pool} }

// Claim atomically takes one claimable job.
//
// FOR UPDATE SKIP LOCKED is what lets N transcoder replicas run with zero
// coordination: each grabs a different row instead of all of them blocking on
// the same one. The subquery picks the row, the outer UPDATE takes it, and both
// happen in one statement so there is no window between select and claim.
//
// The ORDER BY prefers QUEUED work over expired leases, so a healthy backlog is
// drained before retrying something that already failed once.
const claimSQL = `
WITH claimed AS (
    UPDATE transcode_jobs
       SET status           = 'RUNNING',
           worker_id        = $1,
           lease_expires_at = now() + ($2::double precision * interval '1 second'),
           attempt          = attempt + 1,
           started_at       = COALESCE(started_at, now())
     WHERE id = (
         SELECT id
           FROM transcode_jobs
          WHERE status = 'QUEUED'
             OR (status = 'RUNNING' AND lease_expires_at < now())
          ORDER BY (status = 'RUNNING'), created_at
            FOR UPDATE SKIP LOCKED
          LIMIT 1
     )
 RETURNING id, asset_id, pipeline_version, attempt
)
SELECT c.id, c.asset_id, c.pipeline_version, c.attempt,
       a.storage_key, a.content_type, a.purpose
  FROM claimed c
  JOIN media_assets a ON a.id = c.asset_id`

func (r *JobRepo) Claim(ctx context.Context, workerID string, leaseTTL time.Duration) (*transcode.Job, error) {
	var j transcode.Job
	var purpose string

	err := r.pool.QueryRow(ctx, claimSQL, workerID, leaseTTL.Seconds()).Scan(
		&j.ID, &j.AssetID, &j.PipelineVersion, &j.Attempt,
		&j.StorageKey, &j.ContentType, &purpose,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // nothing to do
	}
	if err != nil {
		return nil, err
	}
	j.Purpose = transcode.Purpose(purpose)
	return &j, nil
}

// Heartbeat extends the lease. The worker_id predicate is a fencing check: once
// another worker has reclaimed this job, this UPDATE matches zero rows and the
// caller must abort rather than keep writing rendition keys.
const heartbeatSQL = `
UPDATE transcode_jobs
   SET lease_expires_at = now() + ($3::double precision * interval '1 second'),
       progress = $4, speed = $5, tasks = $6
 WHERE id = $1 AND worker_id = $2 AND status = 'RUNNING'`

func (r *JobRepo) Heartbeat(
	ctx context.Context, jobID uuid.UUID, workerID string,
	leaseTTL time.Duration, progress, speed float64, tasks []byte,
) (bool, error) {
	tag, err := r.pool.Exec(ctx, heartbeatSQL, jobID, workerID, leaseTTL.Seconds(), progress, speed, tasks)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// Succeed marks the job done and the asset READY, in one transaction. An outbox
// row goes in the same transaction so the event cannot be lost if the process
// dies immediately after commit.
func (r *JobRepo) Succeed(
	ctx context.Context, jobID, assetID uuid.UUID, workerID string,
	masterKey, posterKey string, renditions []byte, probe []byte,
	durationSecs float64, width, height, rotation int,
) error {
	return r.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE transcode_jobs
			   SET status = 'SUCCEEDED', progress = 1, finished_at = now(),
			       lease_expires_at = NULL, error = NULL
			 WHERE id = $1 AND worker_id = $2`, jobID, workerID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return transcode.ErrJobStolen
		}

		var poster *string
		if posterKey != "" {
			poster = &posterKey
		}
		if _, err := tx.Exec(ctx, `
			UPDATE media_assets
			   SET status = 'READY', master_key = $2, poster_key = $3,
			       renditions = $4, probe = $5, duration_secs = $6,
			       width = $7, height = $8, rotation = $9, reject_reason = NULL
			 WHERE id = $1`,
			assetID, masterKey, poster, renditions, probe,
			durationSecs, width, height, rotation); err != nil {
			return err
		}

		return insertOutbox(ctx, tx, "media.transcode.completed", assetID, map[string]any{
			"asset_id":   assetID,
			"master_key": masterKey,
			"duration":   durationSecs,
		})
	})
}

// Fail records a retryable or terminal failure. A retryable failure returns the
// job to QUEUED with the lease released, so any worker can pick it up.
func (r *JobRepo) Fail(ctx context.Context, jobID uuid.UUID, workerID, reason string, retryable bool) error {
	if retryable {
		_, err := r.pool.Exec(ctx, `
			UPDATE transcode_jobs
			   SET status = 'QUEUED', worker_id = NULL, lease_expires_at = NULL, error = $3
			 WHERE id = $1 AND worker_id = $2`, jobID, workerID, reason)
		return err
	}

	return r.inTx(ctx, func(tx pgx.Tx) error {
		var assetID uuid.UUID
		err := tx.QueryRow(ctx, `
			UPDATE transcode_jobs
			   SET status = 'FAILED', finished_at = now(), lease_expires_at = NULL, error = $3
			 WHERE id = $1 AND worker_id = $2
			RETURNING asset_id`, jobID, workerID, reason).Scan(&assetID)
		if errors.Is(err, pgx.ErrNoRows) {
			return transcode.ErrJobStolen
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE media_assets SET status = 'FAILED' WHERE id = $1`, assetID); err != nil {
			return err
		}
		return insertOutbox(ctx, tx, "media.transcode.failed", assetID, map[string]any{
			"asset_id": assetID,
			"error":    reason,
		})
	})
}

// Reject marks the asset permanently unusable. Never retried: re-running a file
// with no video stream will fail exactly the same way.
func (r *JobRepo) Reject(
	ctx context.Context, jobID, assetID uuid.UUID, workerID string,
	reason transcode.RejectReason, detail string, probe []byte,
) error {
	return r.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE transcode_jobs
			   SET status = 'CANCELLED', finished_at = now(),
			       lease_expires_at = NULL, error = $3
			 WHERE id = $1 AND worker_id = $2`, jobID, workerID, string(reason)+": "+detail)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return transcode.ErrJobStolen
		}

		var probeArg any
		if len(probe) > 0 {
			probeArg = probe
		}
		if _, err := tx.Exec(ctx, `
			UPDATE media_assets
			   SET status = 'REJECTED', reject_reason = $2, probe = COALESCE($3, probe)
			 WHERE id = $1`, assetID, string(reason), probeArg); err != nil {
			return err
		}
		return insertOutbox(ctx, tx, "media.transcode.rejected", assetID, map[string]any{
			"asset_id": assetID,
			"reason":   string(reason),
			"detail":   detail,
		})
	})
}

// Enqueue creates the job for an uploaded asset.
//
// uq_job_asset_version makes this idempotent: the same Kafka message delivered
// twice produces one job, which is test M8. Delivery is at-least-once, so the
// constraint rather than a hopeful existence check is what makes the consumer
// safe.
func (r *JobRepo) Enqueue(ctx context.Context, assetID uuid.UUID, pipelineVersion int) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		INSERT INTO transcode_jobs (id, asset_id, pipeline_version)
		VALUES ($1, $2, $3)
		ON CONFLICT (asset_id, pipeline_version) DO UPDATE SET asset_id = EXCLUDED.asset_id
		RETURNING id`, uuid.New(), assetID, pipelineVersion).Scan(&id)
	return id, err
}

func insertOutbox(ctx context.Context, tx pgx.Tx, eventType string, aggregateID uuid.UUID, payload map[string]any) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO outbox (event_id, event_type, aggregate_type, aggregate_id, payload)
		VALUES ($1, $2, 'media_asset', $3, $4)`,
		uuid.New(), eventType, aggregateID, payload)
	return err
}

func (r *JobRepo) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}
