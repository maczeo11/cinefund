package pledge

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// in-memory version of Queries for unit tests.
// mirrors the uq_provider_event constraint so idempotency logic is testable.
type fakeQueries struct {
	mu sync.Mutex

	pledges  map[uuid.UUID]*Pledge
	byOrder  map[string]*Pledge
	campaign map[uuid.UUID]*Campaign
	tiers    map[uuid.UUID]*Tier
	raised   map[uuid.UUID]int64

	// processed provider_event_ids - emulates uq_provider_event.
	events   map[string]bool
	balances map[string]int64 // ledger account kind -> balance
	outbox   []OutboxEvent
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{
		pledges:  make(map[uuid.UUID]*Pledge),
		byOrder:  make(map[string]*Pledge),
		campaign: make(map[uuid.UUID]*Campaign),
		tiers:    make(map[uuid.UUID]*Tier),
		raised:   make(map[uuid.UUID]int64),
		events:   make(map[string]bool),
		balances: make(map[string]int64),
	}
}

func (f *fakeQueries) seedCampaign(c *Campaign) { f.campaign[c.ID] = c }
func (f *fakeQueries) seedTier(t *Tier)         { f.tiers[t.ID] = t }
func (f *fakeQueries) seedPledge(p *Pledge) {
	f.pledges[p.ID] = p
	if p.ProviderOrderID != "" {
		f.byOrder[p.ProviderOrderID] = p
	}
}

func (f *fakeQueries) OutboxLen() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.outbox) }
func (f *fakeQueries) Raised(id uuid.UUID) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.raised[id]
}
func (f *fakeQueries) Balance(kind AccountKind) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(kind))
	return f.balances[id.String()]
}
func (f *fakeQueries) Pledge(id uuid.UUID) *Pledge {
	f.mu.Lock()
	defer f.mu.Unlock()
	return clonePledge(f.pledges[id])
}
func (f *fakeQueries) TierClaimed(id uuid.UUID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tiers[id]
	if !ok {
		return 0
	}
	return t.ClaimedCount
}

func (f *fakeQueries) InsertPledge(_ context.Context, p *Pledge) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pledges[p.ID] = p
	if p.ProviderOrderID != "" {
		f.byOrder[p.ProviderOrderID] = p
	}
	return nil
}

func (f *fakeQueries) GetPledgeForUpdate(_ context.Context, id uuid.UUID) (*Pledge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.pledges[id]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return clonePledge(p), nil
}

func (f *fakeQueries) GetPledgeByOrderID(_ context.Context, orderID string) (*Pledge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byOrder[orderID]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return clonePledge(p), nil
}

func (f *fakeQueries) GetCampaignForUpdate(_ context.Context, id uuid.UUID) (*Campaign, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.campaign[id]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return &Campaign{ID: c.ID, CreatorID: c.CreatorID, Status: c.Status, Deadline: c.Deadline}, nil
}

func (f *fakeQueries) GetTierForUpdate(_ context.Context, id uuid.UUID) (*Tier, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tiers[id]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return &Tier{ID: t.ID, CampaignID: t.CampaignID, MinAmount: t.MinAmount,
		QuantityLimit: t.QuantityLimit, ClaimedCount: t.ClaimedCount}, nil
}

func (f *fakeQueries) MarkPledgeCaptured(_ context.Context, id uuid.UUID, paymentID string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.pledges[id]
	if !ok {
		return pgx.ErrNoRows
	}
	p.Status = StatusCaptured
	p.ProviderPaymentID = paymentID
	p.CapturedAt = &at
	return nil
}

func (f *fakeQueries) SetPledgeStatus(_ context.Context, id uuid.UUID, s Status) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.pledges[id]
	if !ok {
		return pgx.ErrNoRows
	}
	p.Status = s
	return nil
}

func (f *fakeQueries) IncrementCampaignRaised(_ context.Context, campaignID uuid.UUID, amount int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.raised[campaignID] += amount
	return nil
}

func (f *fakeQueries) IncrementTierClaimed(_ context.Context, tierID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tiers[tierID]
	if !ok {
		return pgx.ErrNoRows
	}
	t.ClaimedCount++
	return nil
}

func (f *fakeQueries) InsertPaymentEvent(_ context.Context, evt PaymentEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := evt.Provider + ":" + evt.ProviderEventID
	if f.events[key] {
		return &pgconn.PgError{Code: "23505", ConstraintName: "uq_provider_event"}
	}
	f.events[key] = true
	return nil
}

func (f *fakeQueries) InsertLedgerTransaction(_ context.Context, t LedgerTxn) (uuid.UUID, error) {
	return uuid.New(), nil
}

func (f *fakeQueries) InsertLedgerEntries(_ context.Context, entries []LedgerEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range entries {
		if e.Direction == "DEBIT" {
			f.balances[accountKey(e)] -= e.Amount
		} else {
			f.balances[accountKey(e)] += e.Amount
		}
	}
	return nil
}

func (f *fakeQueries) GetOrCreateAccount(_ context.Context, kind AccountKind, _ *uuid.UUID) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(kind))
	if _, ok := f.balances[id.String()]; !ok {
		f.balances[id.String()] = 0
	}
	return id, nil
}

func accountKey(e LedgerEntry) string { return e.AccountID.String() }

func (f *fakeQueries) AttachOrder(_ context.Context, pledgeID uuid.UUID, orderID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.pledges[pledgeID]
	if !ok {
		return pgx.ErrNoRows
	}
	p.ProviderOrderID = orderID
	f.byOrder[orderID] = p
	return nil
}

// no real rollback but good enough for unit tests
func (f *fakeQueries) Do(_ context.Context, fn func(q Queries) error) error {
	return fn(f)
}

func (f *fakeQueries) InsertOutbox(_ context.Context, evt OutboxEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outbox = append(f.outbox, evt)
	return nil
}

func clonePledge(p *Pledge) *Pledge {
	if p == nil {
		return nil
	}
	c := *p
	if p.TierID != nil {
		t := *p.TierID
		c.TierID = &t
	}
	if p.CapturedAt != nil {
		t := *p.CapturedAt
		c.CapturedAt = &t
	}
	if p.RefundedAt != nil {
		t := *p.RefundedAt
		c.RefundedAt = &t
	}
	return &c
}
