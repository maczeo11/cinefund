package pledge

import (
	"time"

	"github.com/google/uuid"
)

// PaymentEvent is one accepted webhook, durable in payment_events. The unique
// constraint uq_provider_event is THE idempotency guarantee; see webhook.go.
type PaymentEvent struct {
	ID             uuid.UUID
	Provider       string
	ProviderEventID string
	EventType      string
	PledgeID       *uuid.UUID
	Payload        []byte // JSONB
	SignatureValid bool
	ProcessedAt    time.Time
}

// OutboxEvent is one row the service writes into the transactional outbox,
// inside the same transaction as the domain write. A dispatcher moves it to
// Kafka afterwards; the two are decoupled by design (docs/08).
type OutboxEvent struct {
	ID            uuid.UUID
	Type          string
	Version       int
	AggregateType string
	AggregateID   uuid.UUID
	Payload       []byte // JSONB
	TraceID       string
}
