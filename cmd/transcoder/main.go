// cmd/transcoder claims transcode jobs and runs FFmpeg.
//
// It is a separate binary from the API for one reason: FFmpeg will happily
// consume every core on the host, and an API sharing a process with it stops
// answering health checks. Give this container a CPU limit.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/maczeo11/cinefund/internal/media"
	"github.com/maczeo11/cinefund/internal/media/transcode"
	"github.com/maczeo11/cinefund/internal/platform/config"
	"github.com/maczeo11/cinefund/internal/platform/logger"
	"github.com/maczeo11/cinefund/internal/platform/objectstore"
	"github.com/maczeo11/cinefund/internal/platform/postgres"
)

var version = "dev"

func main() {
	cfg := config.MustLoad()
	log := logger.New(cfg.Env, "transcoder", version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pg := postgres.MustConnect(connectCtx, cfg.Postgres.DSN, cfg.Postgres.MaxConns)
	defer pg.Close()

	store, err := objectstore.New(objectstore.Config{
		Endpoint:       cfg.S3.Endpoint,
		PublicEndpoint: cfg.S3.PublicEndpoint,
		AccessKey:      cfg.S3.AccessKey,
		SecretKey:      cfg.S3.SecretKey,
		Region:         cfg.S3.Region,
	}, cfg.S3.BucketOriginals)
	if err != nil {
		log.Error("object store", "error", err)
		os.Exit(1)
	}

	worker := transcode.NewWorker(
		transcode.Config{
			Concurrency: cfg.Transcode.Concurrency,
			WorkDir:     cfg.Transcode.TmpDir,
		},
		media.NewJobRepo(pg),
		store,
		transcode.NewRunner(os.Getenv("FFMPEG_PATH")),
		transcode.NewProber(os.Getenv("FFPROBE_PATH")),
		log,
	)

	if err := worker.Run(ctx); err != nil {
		log.Error("worker stopped with error", "error", err)
		os.Exit(1)
	}
	slog.SetDefault(log)
}
