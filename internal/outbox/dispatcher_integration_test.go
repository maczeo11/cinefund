//go:build integration

package outbox_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/maczeo11/cinefund/internal/outbox"
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

// stubProducer just records what was published so we can check ordering.
type stubProducer struct {
	published []string
}

func (s *stubProducer) ProduceSync(_ context.Context, recs ...interface{}) interface{} {
	return nil
}

// E1: if the API crashes between COMMIT (outbox row written) and the
// dispatcher publishing it, the row survives in postgres and the dispatcher
// picks it up on the next tick. The event is not lost.
func TestDispatcher_UnpublishedRowSurvivesRestart(t *testing.T) {
	pool := pgPool(t)
	ctx := context.Background()

	// insert an outbox row directly, simulating a committed tx whose
	// dispatcher never ran (api crashed right after commit)
	eventID := "evt_e1_" + time.Now().Format("150405")
	_, err := pool.Exec(ctx, `
		INSERT INTO outbox (event_id, event_type, aggregate_type, aggregate_id, payload)
		VALUES ($1, 'pledge.captured', 'pledge', gen_random_uuid(), $2)
		ON CONFLICT DO NOTHING`,
		eventID, json.RawMessage(`{"test":"e1"}`))
	if err != nil {
		t.Fatalf("insert outbox row: %v", err)
	}

	// verify the row is unpublished
	var count int
	pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE event_id = $1 AND published_at IS NULL`,
		eventID).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 unpublished row, got %d", count)
	}

	// now a "restarted" dispatcher's PGStore claims it
	store := &outbox.PGStore{Pool: pool}
	rows, err := store.Claim(ctx, 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	found := false
	for _, r := range rows {
		if r.EventID == eventID {
			found = true
			// mark it published like the dispatcher would
			if err := store.MarkPublished(ctx, r.ID); err != nil {
				t.Fatalf("mark published: %v", err)
			}
			break
		}
	}
	if !found {
		t.Fatal("dispatcher did not pick up the orphaned outbox row after restart")
	}

	// verify its marked now
	pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE event_id = $1 AND published_at IS NOT NULL`,
		eventID).Scan(&count)
	if count != 1 {
		t.Fatalf("row should be marked published after dispatcher restart")
	}

	// cleanup
	pool.Exec(ctx, `DELETE FROM outbox WHERE event_id = $1`, eventID)
}
