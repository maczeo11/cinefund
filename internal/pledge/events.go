package pledge

import (
	"time"

	"github.com/google/uuid"
)

// PaymentEvent is one accepted webhook row.
type PaymentEvent struct {
	ID              uuid.UUID
	Provider        string
	ProviderEventID string
	EventType       string
	PledgeID        *uuid.UUID
	Payload         []byte // JSONB
	SignatureValid  bool
	ProcessedAt     time.Time
}

// OutboxEvent goes into the outbox table in the same tx as the domain write.
type OutboxEvent struct {
	ID            uuid.UUID
	Type          string
	Version       int
	AggregateType string
	AggregateID   uuid.UUID
	Payload       []byte // JSONB
	TraceID       string
}
