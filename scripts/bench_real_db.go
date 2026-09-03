// bench_real_db: measures pledge webhook latency against real Postgres+Redis.
// Run with infra up: docker compose -f deploy/docker-compose.yml up -d
// Then: go run ./scripts/bench_real_db.go -n 200 -c 20

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/maczeo11/cinefund/internal/pledge"
	"github.com/maczeo11/cinefund/internal/pledge/gateway/fake"
)

type redisAdapter struct{ c *goredis.Client }

func (a *redisAdapter) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	return a.c.SetNX(ctx, key, value, ttl).Result()
}
func (a *redisAdapter) Del(ctx context.Context, keys ...string) (int64, error) {
	return a.c.Del(ctx, keys...).Result()
}

func main() {
	var n = flag.Int("n", 200, "total webhook calls (unique pledges)")
	var c = flag.Int("c", 20, "concurrency")
	flag.Parse()

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://cinefund:cinefund@localhost:5433/cinefund?sslmode=disable" //nolint:gosec // dev benchmark fallback
	}
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pg connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "pg ping: %v\n", err)
		os.Exit(1)
	}

	rdb := goredis.NewClient(&goredis.Options{Addr: redisAddr, DB: 2})
	defer func() { _ = rdb.Close() }()
	_ = rdb.FlushDB(ctx).Err()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := pledge.NewRepo(pool)
	gw := fake.New()
	secret := "bench-secret"
	rd := &redisAdapter{c: rdb}
	svc := pledge.NewService(repo, repo, pledge.NewLedger(), gw, rd, pledge.Secrets{WebhookSecret: secret}, log)

	// seed one campaign + creator
	campaignID := uuid.New()
	creatorID := uuid.New()
	_, _ = pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, display_name) VALUES ($1,$2,'','Bench Creator') ON CONFLICT DO NOTHING`, creatorID, "bench-creator-"+campaignID.String()[:8]+"@cinefund.dev")
	_, err = pool.Exec(ctx, `INSERT INTO campaigns (id, creator_id, slug, title, tagline, synopsis, category, status, deadline, published_at, goal_amount)
		VALUES ($1,$2,$3,'Bench Campaign','bench tagline','synopsis for bench','DRAMA','LIVE', $4, now(), 1000000)`, campaignID, creatorID, "bench-"+campaignID.String()[:8], time.Now().Add(48*time.Hour))
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed campaign: %v\n", err)
		os.Exit(1)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM campaigns WHERE id=$1`, campaignID) }()
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, creatorID) }()

	// create n pledges (each with its own backer + order + webhook body)
	type job struct {
		body []byte
	}
	jobs := make([]job, *n)
	for i := 0; i < *n; i++ {
		backerID := uuid.New()
		_, _ = pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, display_name) VALUES ($1,$2,'','Bench Backer') ON CONFLICT DO NOTHING`, backerID, "bench-backer-"+backerID.String()[:8]+"@cinefund.dev")
		p, err := svc.CreatePledge(ctx, pledge.CreateInput{CampaignID: campaignID, BackerID: backerID, Amount: 50000})
		if err != nil {
			fmt.Fprintf(os.Stderr, "create pledge %d: %v\n", i, err)
			os.Exit(1)
		}
		body, _, _ := gw.Capture(p.ProviderOrderID, fake.PaymentEntity{ID: "pay_bench_" + fmt.Sprint(i), Amount: 50000, Fee: 1000, Tax: 180, Status: "captured"}, secret)
		jobs[i] = job{body: body}
		// track backer for cleanup
		defer func(bID uuid.UUID) { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, bID) }(backerID)
	}

	// also track pledges cleanup via campaign cascade? just delete by campaign
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM pledges WHERE campaign_id=$1`, campaignID) }()
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM payment_events WHERE provider='razorpay'`) }()

	durs := make([]time.Duration, 0, *n)
	var mu sync.Mutex
	sem := make(chan struct{}, *c)
	var wg sync.WaitGroup
	failures := 0
	var failMu sync.Mutex

	start := time.Now()
	for i := 0; i < *n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			t0 := time.Now()
			err := svc.HandleWebhook(ctx, jobs[idx].body)
			d := time.Since(t0)
			mu.Lock()
			durs = append(durs, d)
			mu.Unlock()
			if err != nil {
				failMu.Lock()
				failures++
				failMu.Unlock()
				fmt.Fprintf(os.Stderr, "webhook %d err: %v\n", idx, err)
			}
		}(i)
	}
	wg.Wait()
	total := time.Since(start)

	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
	if len(durs) == 0 {
		fmt.Println("no durations")
		return
	}
	p50 := durs[len(durs)*50/100]
	p95 := durs[len(durs)*95/100]
	p99 := durs[len(durs)*99/100]
	var sum time.Duration
	for _, d := range durs {
		sum += d
	}
	avg := sum / time.Duration(len(durs))
	rps := float64(*n) / total.Seconds()

	fmt.Printf("real DB bench  n=%d c=%d failures=%d\n", *n, *c, failures)
	fmt.Printf("total %v (%.1f req/s)\n", total, rps)
	fmt.Printf("min %v  p50 %v  p95 %v  p99 %v  max %v  avg %v\n", durs[0], p50, p95, p99, durs[len(durs)-1], avg)
	fmt.Printf("resume: p50 %.1fms p95 %.1fms p99 %.1fms (%d concurrent, real Postgres+Redis)\n", float64(p50.Microseconds())/1000, float64(p95.Microseconds())/1000, float64(p99.Microseconds())/1000, *c)
}
