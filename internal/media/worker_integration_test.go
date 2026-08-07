//go:build integration

package media_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/maczeo11/cinefund/internal/media"
)

func pgPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://cinefund:cinefund@localhost:5433/cinefund?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect pg: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// M7: worker A claims a job, then dies (lease expires). Worker B should be
// able to reclaim the same job via SKIP LOCKED. When worker A tries to
// heartbeat after that, it gets 0 rows affected (fenced out).
func TestWorkerReclaim_DeadWorkerFencedOut(t *testing.T) {
	pool := pgPool(t)
	ctx := context.Background()
	repo := media.NewJobRepo(pool)

	// seed a media asset so the JOIN in the claim query works
	assetID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO media_assets (id, uploader_id, storage_key, content_type, purpose, status)
		VALUES ($1, $2, 'test/sample.mp4', 'video/mp4', 'CAMPAIGN_VIDEO', 'UPLOADED')
		ON CONFLICT DO NOTHING`,
		assetID, uuid.New())
	if err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM transcode_jobs WHERE asset_id = $1`, assetID)
		pool.Exec(ctx, `DELETE FROM media_assets WHERE id = $1`, assetID)
	})

	// enqueue a job
	jobID, err := repo.Enqueue(ctx, assetID, 1)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// worker A claims with a very short lease (1 second)
	workerA := "worker-a-" + uuid.NewString()[:8]
	job, err := repo.Claim(ctx, workerA, 1*time.Second)
	if err != nil {
		t.Fatalf("worker A claim: %v", err)
	}
	if job == nil {
		t.Fatal("worker A got nil job")
	}
	if job.ID != jobID {
		t.Fatalf("worker A got wrong job: %s vs %s", job.ID, jobID)
	}

	// wait for lease to expire
	time.Sleep(2 * time.Second)

	// worker B reclaims the expired-lease job
	workerB := "worker-b-" + uuid.NewString()[:8]
	job2, err := repo.Claim(ctx, workerB, 60*time.Second)
	if err != nil {
		t.Fatalf("worker B claim: %v", err)
	}
	if job2 == nil {
		t.Fatal("worker B should have reclaimed the expired job")
	}
	if job2.ID != jobID {
		t.Fatalf("worker B got wrong job: %s vs %s", job2.ID, jobID)
	}

	// worker A tries to heartbeat — should be fenced out (0 rows)
	ok, err := repo.Heartbeat(ctx, jobID, workerA, 60*time.Second, 0.5, 1.0, nil)
	if err != nil {
		t.Fatalf("worker A heartbeat error: %v", err)
	}
	if ok {
		t.Fatal("worker A heartbeat should return false (fenced out), but got true")
	}

	// worker B heartbeat should still work
	ok, err = repo.Heartbeat(ctx, jobID, workerB, 60*time.Second, 0.1, 0.5, nil)
	if err != nil {
		t.Fatalf("worker B heartbeat error: %v", err)
	}
	if !ok {
		t.Fatal("worker B heartbeat should succeed")
	}
}
