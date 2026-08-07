// Package outbox polls unpublished rows and sends them to Kafka.
package outbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
)

type Row struct {
	ID          int64
	EventID     string
	EventType   string
	AggregateID string
	Payload     json.RawMessage
}

type Producer interface {
	ProduceSync(ctx context.Context, recs ...*kgo.Record) kgo.ProduceResults
}

type Store interface {
	Claim(ctx context.Context, n int) ([]Row, error)
	MarkPublished(ctx context.Context, id int64) error
}
type Dispatcher struct {
	store  Store
	prod   Producer
	topic  string
	batch  int
	ticker time.Duration
	log    *slog.Logger
}

func New(store Store, prod Producer, topic string, batch int, ticker time.Duration, log *slog.Logger) *Dispatcher {
	return &Dispatcher{store: store, prod: prod, topic: topic, batch: batch, ticker: ticker, log: log}
}

// Run polls until ctx is done.
func (d *Dispatcher) Run(ctx context.Context) error {
	d.log.Info("dispatcher started", "topic", d.topic, "batch", d.batch)
	t := time.NewTicker(d.ticker)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			d.log.Info("dispatcher stopping")
			return nil
		case <-t.C:
			if err := d.tick(ctx); err != nil {
				d.log.Error("tick failed", "error", err)
			}
		}
	}
}

// tick publishes one batch.
func (d *Dispatcher) tick(ctx context.Context) error {
	rows, err := d.store.Claim(ctx, d.batch)
	if err != nil {
		return err
	}
	// TODO: batch produce instead of one at a time
	for _, r := range rows {
		key := []byte(r.AggregateID)
		value, _ := json.Marshal(map[string]any{
			"event_id":     r.EventID,
			"event_type":   r.EventType,
			"aggregate_id": r.AggregateID,
			"payload":      json.RawMessage(r.Payload),
			"published_at": time.Now().UTC(),
		})
		rec := &kgo.Record{Topic: d.topic, Key: key, Value: value}
		if res := d.prod.ProduceSync(ctx, rec); res.FirstErr() != nil {
			d.log.Error("produce failed", "event_id", r.EventID, "error", res.FirstErr())
			continue
		}
		if err := d.store.MarkPublished(ctx, r.ID); err != nil {
			d.log.Error("mark published failed", "event_id", r.EventID, "error", err)
		}
	}
	return nil
}

// SKIP LOCKED so two dispatchers never grab the same row
const claimSQL = `
WITH claimed AS (
    SELECT id FROM outbox
     WHERE published_at IS NULL
     ORDER BY id
       FOR UPDATE SKIP LOCKED
     LIMIT $1
)
SELECT c.id, o.event_id, o.event_type, o.aggregate_id, o.payload
  FROM claimed c
  JOIN outbox o ON o.id = c.id`

// PGStore is the Postgres implementation.
type PGStore struct{ Pool *pgxpool.Pool }

func (s *PGStore) Claim(ctx context.Context, n int) ([]Row, error) {
	rows, err := s.Pool.Query(ctx, claimSQL, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.ID, &r.EventID, &r.EventType, &r.AggregateID, &r.Payload); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkPublished is idempotent: publishing the same row twice is harmless, and
// the WHERE keeps a replica from un-publishing someone else's row.
func (s *PGStore) MarkPublished(ctx context.Context, id int64) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE outbox SET published_at = now()
		 WHERE id = $1 AND published_at IS NULL`, id)
	return err
}
