package pledge

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestLatencyReport prints p50/p95/p99 for the webhook idempotency path.
// Run: go test -run TestLatencyReport -v ./internal/pledge
// This is NOT a benchmark — it measures the fake (no DB/network) path so you
// can paste a real number into the resume without fabricating one.
func TestLatencyReport(t *testing.T) {
	cases := []struct {
		name string
		n    int
		c    int
	}{
		{"50x_same_event", 50, 50},
		{"1000x_same_event_c50", 1000, 50},
		{"1000x_unique_events_c50", 1000, 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fq := newFakeQueries()
			c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
			fq.seedCampaign(c)
			svc, gw, _ := testService(fq)

			// Seed enough pledges/orders for unique-events case
			var bodies [][]byte
			if tc.name == "1000x_unique_events_c50" {
				bodies = make([][]byte, tc.n)
				for i := 0; i < tc.n; i++ {
					p, err := svc.CreatePledge(context.Background(), CreateInput{
						CampaignID: c.ID, BackerID: uuid.New(), Amount: 100000,
					})
					if err != nil {
						t.Fatalf("CreatePledge %d: %v", i, err)
					}
					b, _ := captureBody(t, gw, p.ProviderOrderID, 100000, testSecret)
					bodies[i] = b
				}
			} else {
				p, err := svc.CreatePledge(context.Background(), CreateInput{
					CampaignID: c.ID, BackerID: uuid.New(), Amount: 100000,
				})
				if err != nil {
					t.Fatalf("CreatePledge: %v", err)
				}
				b, _ := captureBody(t, gw, p.ProviderOrderID, 100000, testSecret)
				// warmup: first delivery succeeds, then reset events so bench starts at the contended point
				if err := svc.HandleWebhook(context.Background(), b); err != nil {
					t.Fatalf("warmup: %v", err)
				}
				fq.mu.Lock()
				fq.events = map[string]bool{}
				fq.mu.Unlock()
				// also clear redis key
				bodies = make([][]byte, tc.n)
				for i := range bodies {
					bodies[i] = b
				}
				// clear redis by creating a fresh service/store
				// simplest: recreate redis
				svc2, _, _ := testService(fq)
				// re-capture body with fresh gateway sig (same event_id would be dup otherwise)
				// Actually need a fresh pledge for the measured run:
				fq2 := newFakeQueries()
				fq2.seedCampaign(c)
				svc, gw, _ = testService(fq2)
				p2, _ := svc.CreatePledge(context.Background(), CreateInput{CampaignID: c.ID, BackerID: uuid.New(), Amount: 100000})
				b2, _ := captureBody(t, gw, p2.ProviderOrderID, 100000, testSecret)
				for i := range bodies {
					bodies[i] = b2
				}
				_ = svc2
			}

			durs := runConcurrent(t, svc, bodies, tc.c)
			report(t, tc.name, durs, tc.c)
		})
	}
}

func runConcurrent(t *testing.T, svc *Service, bodies [][]byte, concurrency int) []time.Duration {
	t.Helper()
	n := len(bodies)
	durs := make([]time.Duration, 0, n)
	var mu sync.Mutex
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			start := time.Now()
			err := svc.HandleWebhook(context.Background(), bodies[idx])
			d := time.Since(start)
			// ErrDuplicateEvent is expected for same-event contended bench
			if err != nil && err.Error() != "duplicate event" {
				t.Errorf("HandleWebhook %d: %v", idx, err)
			}
			mu.Lock()
			durs = append(durs, d)
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	return durs
}

func report(t *testing.T, name string, durs []time.Duration, conc int) {
	t.Helper()
	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
	n := len(durs)
	p50 := durs[n*50/100]
	p95 := durs[n*95/100]
	p99 := durs[n*99/100]
	var sum time.Duration
	for _, d := range durs {
		sum += d
	}
	avg := sum / time.Duration(n)
	min, max := durs[0], durs[n-1]
	rps := float64(n) / sum.Seconds() * float64(n) // not meaningful for fake, but total time is more useful
	_ = rps
	t.Logf("— %s  n=%d c=%d", name, n, conc)
	t.Logf("  min %v  p50 %v  p95 %v  p99 %v  max %v  avg %v", min, p50, p95, p99, max, avg)
	t.Logf("  resume: p50 %.1fms p95 %.1fms p99 %.1fms (%d concurrent, %d req, fake/no-DB)", ms(p50), ms(p95), ms(p99), conc, n)
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func latencyEventID(t *testing.T, body []byte) string {
	t.Helper()
	return string(body[:minLatency(40, len(body))])
}
func minLatency(a, b int) int {
	if a < b {
		return a
	}
	return b
}
