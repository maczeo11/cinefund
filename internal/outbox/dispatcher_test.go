package outbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

type fakeStore struct {
	mu    sync.Mutex
	rows  []Row
	done  map[int64]bool
	marks []int64
}

func (f *fakeStore) Claim(_ context.Context, n int) ([]Row, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Row
	for _, r := range f.rows {
		if len(out) >= n {
			break
		}
		if !f.done[r.ID] {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeStore) MarkPublished(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.done[id] = true
	f.marks = append(f.marks, id)
	return nil
}

type fakeProducer struct {
	mu      sync.Mutex
	records []*kgo.Record
}

func (f *fakeProducer) ProduceSync(_ context.Context, recs ...*kgo.Record) kgo.ProduceResults {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, recs...)
	return kgo.ProduceResults{}
}

func TestDispatcher_PublishThenMark(t *testing.T) {
	store := &fakeStore{
		rows: []Row{
			{ID: 1, EventID: "evt_1", EventType: "pledge.captured", AggregateID: "pl_1", Payload: json.RawMessage(`{"amount":100}`)},
			{ID: 2, EventID: "evt_2", EventType: "media.uploaded", AggregateID: "ma_1", Payload: json.RawMessage(`{"asset_id":"ma_1"}`)},
		},
		done: map[int64]bool{},
	}
	prod := &fakeProducer{}

	d := New(store, prod, "cinefund.events", 10, time.Second, slog.Default())
	if err := d.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(prod.records) != 2 {
		t.Fatalf("produced %d records, want 2", len(prod.records))
	}
	var got []string
	for _, rec := range prod.records {
		got = append(got, string(rec.Key))
	}
	if got[0] != "pl_1" || got[1] != "ma_1" {
		t.Errorf("produced keys = %v", got)
	}
	if len(store.marks) != 2 {
		t.Fatalf("marked %d rows, want 2", len(store.marks))
	}
}

// second tick shouldnt republish already-done rows
func TestDispatcher_DoesNotRepublish(t *testing.T) {
	store := &fakeStore{
		rows: []Row{{ID: 1, EventID: "evt_1", EventType: "pledge.captured", AggregateID: "pl_1", Payload: json.RawMessage(`{}`)}},
		done: map[int64]bool{},
	}
	prod := &fakeProducer{}

	d := New(store, prod, "cinefund.events", 10, time.Second, slog.Default())
	_ = d.tick(context.Background())
	_ = d.tick(context.Background())

	if len(prod.records) != 1 {
		t.Errorf("produced %d records over two ticks, want 1", len(prod.records))
	}
}
