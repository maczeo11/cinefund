package media

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Consumer turns Kafka events into transcode jobs.
type Consumer struct {
	client  *kgo.Client
	jobRepo *JobRepo
	topic   string
	log     *slog.Logger
}

// NewConsumer builds a consumer.
func NewConsumer(client *kgo.Client, jobRepo *JobRepo, topic string, log *slog.Logger) *Consumer {
	return &Consumer{client: client, jobRepo: jobRepo, topic: topic, log: log}
}

// event is what the dispatcher writes to kafka.
type event struct {
	EventID   string          `json:"event_id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

// Run consumes until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	c.log.Info("consumer started", "topic", c.topic)
	for {
		fetches := c.client.PollFetches(ctx)
		if err := fetches.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			c.log.Error("poll failed", "error", err)
			continue
		}
		fetches.EachRecord(func(rec *kgo.Record) {
			var evt event
			if err := json.Unmarshal(rec.Value, &evt); err != nil {
				c.log.Warn("bad event on topic", "error", err)
				return
			}
			if evt.EventType == "media.uploaded" {
				c.handleUploaded(ctx, rec, evt)
			}
			if err := c.client.CommitRecords(ctx, rec); err != nil {
				c.log.Warn("commit failed", "error", err)
			}
		})
	}
}

// handleUploaded enqueues a transcode job.
func (c *Consumer) handleUploaded(ctx context.Context, rec *kgo.Record, evt event) {
	var p struct {
		AssetID string `json:"asset_id"`
	}
	if err := json.Unmarshal(evt.Payload, &p); err != nil || p.AssetID == "" {
		c.log.Warn("media.uploaded missing asset_id", "event_id", evt.EventID)
		return
	}
	assetID, err := uuid.Parse(p.AssetID)
	if err != nil {
		c.log.Warn("media.uploaded bad asset_id", "event_id", evt.EventID, "asset_id", p.AssetID)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := c.jobRepo.Enqueue(ctx, assetID, 1); err != nil {
		c.log.Error("enqueue failed", "asset_id", assetID, "error", err)
		return
	}
	c.log.Info("enqueued transcode job", "asset_id", assetID)
}
