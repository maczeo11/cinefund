//go:build integration

package pledge_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/maczeo11/cinefund/internal/pledge"
	"github.com/maczeo11/cinefund/internal/pledge/gateway/fake"
)

// wraps go-redis so it satisfies pledge.Redis interface
type redisAdapter struct{ c *goredis.Client }

func (a *redisAdapter) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	return a.c.SetNX(ctx, key, value, ttl).Result()
}

func (a *redisAdapter) Del(ctx context.Context, keys ...string) (int64, error) {
	return a.c.Del(ctx, keys...).Result()
}

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

func redisClient(t *testing.T) *goredis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	rdb := goredis.NewClient(&goredis.Options{Addr: addr, DB: 1})
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

// P4: postgres goes down between two webhook deliveries. The first attempt
// fails with a 500, but when postgres comes back the retry lands and is
// applied exactly once.
//
// We can't actually kill postgres mid-test easily, so we simulate a transient
// failure by cancelling the context on the first call, then retrying with a
// fresh context. The unique constraint on payment_events ensures exactly-once.
func TestWebhook_PostgresOutage_RetryAppliedOnce(t *testing.T) {
	pool := pgPool(t)
	rdb := redisClient(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// seed a campaign directly
	campaignID := uuid.New()
	creatorID := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO campaigns (id, creator_id, title, status, deadline, goal_amount, raised_amount, backer_count)
		VALUES ($1, $2, 'test campaign', 'LIVE', $3, 1000000, 0, 0)
		ON CONFLICT DO NOTHING`,
		campaignID, creatorID, time.Now().Add(48*time.Hour))
	if err != nil {
		t.Fatalf("seed campaign: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM campaigns WHERE id = $1`, campaignID)
	})

	repo := pledge.NewRepo(pool)
	gw := fake.New()
	secret := "integration-test-secret"

	rd := &redisAdapter{c: rdb}
	svc := pledge.NewService(repo, repo, pledge.NewLedger(), gw, rd,
		pledge.Secrets{WebhookSecret: secret}, log)

	backerID := uuid.New()
	p, err := svc.CreatePledge(context.Background(), pledge.CreateInput{
		CampaignID: campaignID,
		BackerID:   backerID,
		Amount:     50000,
	})
	if err != nil {
		t.Fatalf("create pledge: %v", err)
	}

	body, _, err := gw.Capture(p.ProviderOrderID, fake.PaymentEntity{
		ID: "pay_integ_1", Amount: 50000, Fee: 1000, Tax: 180, Status: "captured",
	}, secret)
	if err != nil {
		t.Fatalf("build capture body: %v", err)
	}

	// first attempt: cancel the context to simulate a transient pg failure
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled
	_ = svc.HandleWebhook(cancelCtx, body)

	// flush redis so the retry isnt blocked by the fast path
	rdb.FlushDB(context.Background())

	// second attempt: should succeed
	if err := svc.HandleWebhook(context.Background(), body); err != nil {
		t.Fatalf("retry after outage: %v", err)
	}

	// a third delivery must be deduplicated
	rdb.FlushDB(context.Background())
	err = svc.HandleWebhook(context.Background(), body)
	if err != nil && err.Error() != "duplicate event" {
		t.Fatalf("third delivery should be dup or nil, got: %v", err)
	}

	// check raised amount is exactly 50000, not doubled
	var raised int64
	pool.QueryRow(context.Background(),
		`SELECT raised_amount FROM campaigns WHERE id = $1`, campaignID).Scan(&raised)
	if raised != 50000 {
		t.Fatalf("raised = %d, want exactly 50000 (applied once)", raised)
	}
}
