// cmd/api is the HTTP server.
//
// Wiring is explicit constructor calls, top to bottom. No DI framework, no
// reflection, no container - so the entire dependency graph reads in one screen
// and when something is nil at 2 a.m. you know where it was supposed to be
// constructed.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/maczeo11/cinefund/internal/platform/config"
	"github.com/maczeo11/cinefund/internal/platform/logger"
	"github.com/maczeo11/cinefund/internal/platform/postgres"
)

var version = "dev" // overridden with -ldflags "-X main.version=..."

func main() {
	cfg := config.MustLoad()
	log := logger.New(cfg.Env, "api", version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pg := postgres.MustConnect(connectCtx, cfg.Postgres.DSN, cfg.Postgres.MaxConns)
	defer pg.Close()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, DB: cfg.Redis.DB})
	defer func() { _ = rdb.Close() }()

	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())

	// /health/live must never touch a dependency. A liveness probe that fails
	// because Postgres is down gets the container killed for someone else's
	// outage. There is a test that asserts this with every dep stopped.
	r.GET("/health/live", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "version": version})
	})

	// /health/ready reports whether this instance can serve traffic. Postgres is
	// required. Redis is NOT: the rate limiter falls back to an in-memory
	// counter and the cache simply misses, so a Redis outage degrades rather
	// than removes the service (ADR-0006).
	r.GET("/health/ready", func(c *gin.Context) {
		checkCtx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		body := gin.H{"postgres": "ok", "redis": "ok"}
		code := http.StatusOK

		if err := pg.Ping(checkCtx); err != nil {
			body["postgres"] = "down"
			code = http.StatusServiceUnavailable
		}
		if err := rdb.Ping(checkCtx).Err(); err != nil {
			body["redis"] = "degraded"
		}
		c.JSON(code, body)
	})

	// TODO(A2+): identity.RegisterRoutes, campaign.RegisterRoutes,
	// pledge.RegisterRoutes, media.RegisterRoutes, playback.RegisterRoutes.

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("http listening", "addr", srv.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	log.Info("stopped")
}
