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

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/maczeo11/cinefund/internal/campaign"
	"github.com/maczeo11/cinefund/internal/media"
	"github.com/maczeo11/cinefund/internal/platform/config"
	"github.com/maczeo11/cinefund/internal/platform/httpx"
	"github.com/maczeo11/cinefund/internal/platform/logger"
	"github.com/maczeo11/cinefund/internal/platform/objectstore"
	"github.com/maczeo11/cinefund/internal/platform/postgres"
	"github.com/maczeo11/cinefund/internal/pledge"
	"github.com/maczeo11/cinefund/internal/pledge/gateway"
	"github.com/maczeo11/cinefund/internal/pledge/gateway/fake"
	"github.com/maczeo11/cinefund/internal/pledge/gateway/razorpay"
)

// adapts go-redis to the pledge.Redis interface
type redisAdapter struct{ *redis.Client }

func (a redisAdapter) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	return a.Client.SetNX(ctx, key, value, ttl).Result()
}

func (a redisAdapter) Del(ctx context.Context, keys ...string) (int64, error) {
	return a.Client.Del(ctx, keys...).Result()
}

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
	// CORS — live backend only, allow all origins (Vercel preview + localhost) — frontend forwards to live data only, no mock
	r.Use(cors.New(cors.Config{
		AllowAllOrigins: true,
		AllowMethods: []string{"GET", "POST", "OPTIONS"},
		AllowHeaders: []string{"Content-Type", "Authorization"},
	}))
	r.Use(httpx.Middleware(log))

	// liveness — no deps, just proves the process is up
	r.GET("/health/live", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "version": version})
	})

	// readiness — postgres required, redis degrades gracefully
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

	// real razorpay when creds are set, otherwise the in-memory fake
	var gw gateway.Gateway
	secrets := pledge.Secrets{
		KeySecret:     cfg.Razorpay.KeySecret,
		WebhookSecret: cfg.Razorpay.WebhookSecret,
	}
	if cfg.UseFakeGateway() {
		log.Info("using fake payment gateway")
		gw = fake.New()
		secrets.KeySecret = "" // nothing signs the fake's checkout callback
	} else {
		gw = razorpay.New(cfg.Razorpay.KeyID, cfg.Razorpay.KeySecret, "")
	}

	pledgeRepo := pledge.NewRepo(pg)
	pledgeSvc := pledge.NewService(
		pledgeRepo,
		pledgeRepo,
		pledge.NewLedger(),
		gw,
		redisAdapter{rdb},
		secrets,
		log,
	)

	campaignStore := campaign.NewStore(pg)
	objStore, err := objectstore.New(objectstore.Config{
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

	uploadStore := media.NewUploadStore(pg, objStore)

	api := r.Group("/api/v1")
	{
		// public config for frontend (razorpay key id, etc.)
		api.GET("/config", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"razorpay_key_id": cfg.Razorpay.KeyID,
			})
		})

		campH := campaign.NewHandler(campaignStore)
		api.GET("/campaigns", campH.List)
		api.POST("/campaigns", campH.Create)
		api.GET("/campaigns/:id", campH.Get)
		api.POST("/campaigns/:id/publish", campH.SetLive)
		api.GET("/campaigns/:id/tiers", campH.Tiers)
		api.POST("/campaigns/:id/tiers", campH.AddTier)

		pledgeH := pledge.NewHandler(pledgeSvc)
		api.POST("/campaigns/:id/pledges", pledgeH.CreatePledge)
		api.POST("/pledges/:id/confirm", pledgeH.Confirm)

		mediaH := media.NewHandler(uploadStore, media.NewJobRepo(pg))
		api.POST("/uploads", mediaH.Presign)
		api.POST("/uploads/:id/complete", mediaH.Complete)
	}
	r.POST("/webhooks/razorpay", pledge.NewHandler(pledgeSvc).Webhook)

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
