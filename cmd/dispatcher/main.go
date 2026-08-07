// cmd/dispatcher moves outbox rows to Kafka.
//
// It is a separate process from the API so a Kafka outage does not slow down
// request handling: events accumulate in the outbox and this binary drains them.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/maczeo11/cinefund/internal/outbox"
	"github.com/maczeo11/cinefund/internal/platform/config"
	"github.com/maczeo11/cinefund/internal/platform/logger"
	"github.com/maczeo11/cinefund/internal/platform/postgres"
)

var version = "dev"

func main() {
	cfg := config.MustLoad()
	log := logger.New(cfg.Env, "dispatcher", version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pg := postgres.MustConnect(connectCtx, cfg.Postgres.DSN, cfg.Postgres.MaxConns)
	defer pg.Close()

	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Kafka.Brokers...),
		kgo.DefaultProduceTopic("cinefund.events"),
	)
	if err != nil {
		log.Error("kafka client", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	d := outbox.New(&outbox.PGStore{Pool: pg}, client, "cinefund.events", 100, 2*time.Second, log)
	if err := d.Run(ctx); err != nil {
		log.Error("dispatcher stopped with error", "error", err)
		os.Exit(1)
	}
}
