// cmd/seed inserts deterministic dev data: a demo user, a live campaign and a
// couple of tiers. Idempotent - safe to run repeatedly.
package main

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/maczeo11/cinefund/internal/campaign"
	"github.com/maczeo11/cinefund/internal/platform/config"
	"github.com/maczeo11/cinefund/internal/platform/postgres"
)

var (
	creatorID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	backerID  = uuid.MustParse("00000000-0000-0000-0000-000000000002")
)

func main() {
	cfg := config.MustLoad()
	ctx := context.Background()

	pg := postgres.MustConnect(ctx, cfg.Postgres.DSN, 5)
	defer pg.Close()

	if err := seed(ctx, pg); err != nil {
		log.Fatalf("seed: %v", err)
	}
	log.Println("seed complete")
}

func seed(ctx context.Context, pool *postgres.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Users first; pledges and campaigns reference them.
	if err := upsertUser(ctx, tx, creatorID, "creator@cinefund.dev", "Ava"); err != nil {
		return err
	}
	if err := upsertUser(ctx, tx, backerID, "backer@cinefund.dev", "Ravi"); err != nil {
		return err
	}

	store := campaign.NewStore(pool)
	c, err := store.Create(ctx, campaign.NewCampaign{
		CreatorID: creatorID,
		Title:     "The Last Frame",
		Tagline:   "a short film about a film that never was",
		Synopsis:  "A projectionist finds a reel of a film nobody made.",
		Category:  "DRAMA",
		Goal:      500000,
	})
	if err != nil {
		return err
	}
	if err := store.SetLive(ctx, c.ID); err != nil {
		return err
	}
	// Give the demo campaign a real deadline rather than the 30-day default so
	// the pledge flow behaves predictably.
	if _, err := tx.Exec(ctx, `
		UPDATE campaigns SET deadline = $2 WHERE id = $1`,
		c.ID, time.Now().Add(45*24*time.Hour)); err != nil {
		return err
	}

	if _, err := store.AddTier(ctx, campaign.NewTier{
		CampaignID: c.ID,
		Title:      "Premiere ticket",
		MinAmount:  100000,
	}); err != nil {
		return err
	}
	if _, err := store.AddTier(ctx, campaign.NewTier{
		CampaignID: c.ID,
		Title:      "Poster + credit",
		MinAmount:  25000,
	}); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func upsertUser(ctx context.Context, tx pgx.Tx, id uuid.UUID, email, name string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, display_name)
		VALUES ($1,$2,'','$3')
		ON CONFLICT (id) DO NOTHING`, id, email, name)
	return err
}
