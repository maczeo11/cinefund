package campaign

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Campaign struct {
	ID           uuid.UUID  `json:"id"`
	CreatorID    uuid.UUID  `json:"creator_id"`
	Title        string     `json:"title"`
	Tagline      string     `json:"tagline"`
	Synopsis     string     `json:"synopsis"`
	Category     string     `json:"category"`
	GoalAmount   int64      `json:"goal_amount"`
	RaisedAmount int64      `json:"raised_amount"`
	BackerCount  int        `json:"backer_count"`
	Status       string     `json:"status"`
	Deadline     *time.Time `json:"deadline"`
}

type Tier struct {
	ID            uuid.UUID `json:"id"`
	CampaignID    uuid.UUID `json:"campaign_id"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	MinAmount     int64     `json:"min_amount"`
	QuantityLimit *int      `json:"quantity_limit"`
	ClaimedCount  int       `json:"claimed_count"`
}

type NewCampaign struct {
	CreatorID uuid.UUID `json:"creator_id"`
	Title     string    `json:"title"`
	Tagline   string    `json:"tagline"`
	Synopsis  string    `json:"synopsis"`
	Category  string    `json:"category"`
	Goal      int64     `json:"goal"`
}

type NewTier struct {
	CampaignID    uuid.UUID `json:"-"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	MinAmount     int64     `json:"min_amount"`
	QuantityLimit *int      `json:"quantity_limit"`
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Create(ctx context.Context, in NewCampaign) (*Campaign, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO campaigns (id, creator_id, slug, title, tagline, synopsis,
		                       category, goal_amount, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'DRAFT')
		RETURNING id`,
		uuid.New(), in.CreatorID, uuid.NewString()[:8], in.Title, in.Tagline,
		in.Synopsis, in.Category, in.Goal).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Store) Get(ctx context.Context, id uuid.UUID) (*Campaign, error) {
	var c Campaign
	err := s.pool.QueryRow(ctx, `
		SELECT id, creator_id, title, tagline, COALESCE(synopsis, ''), category,
		       goal_amount, raised_amount, backer_count, status, deadline
		  FROM campaigns WHERE id = $1`, id).
		Scan(&c.ID, &c.CreatorID, &c.Title, &c.Tagline, &c.Synopsis, &c.Category,
			&c.GoalAmount, &c.RaisedAmount, &c.BackerCount, &c.Status, &c.Deadline)
	return &c, err
}

func (s *Store) List(ctx context.Context) ([]Campaign, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, creator_id, title, tagline, COALESCE(synopsis, ''), category,
		       goal_amount, raised_amount, backer_count, status, deadline
		  FROM campaigns
		 ORDER BY created_at DESC
		 LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Campaign
	for rows.Next() {
		var c Campaign
		if err := rows.Scan(&c.ID, &c.CreatorID, &c.Title, &c.Tagline, &c.Synopsis, &c.Category,
			&c.GoalAmount, &c.RaisedAmount, &c.BackerCount, &c.Status, &c.Deadline); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetLive publishes a campaign. Defaults to 30-day deadline.
func (s *Store) SetLive(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE campaigns
		   SET status = 'LIVE',
		       deadline = COALESCE(deadline, now() + interval '30 days'),
		       published_at = COALESCE(published_at, now())
		 WHERE id = $1`, id)
	return err
}

func (s *Store) AddTier(ctx context.Context, in NewTier) (*Tier, error) {
	var t Tier
	err := s.pool.QueryRow(ctx, `
		INSERT INTO reward_tiers (id, campaign_id, title, description, min_amount, quantity_limit)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, campaign_id, title, COALESCE(description, ''), min_amount, quantity_limit, claimed_count`,
		uuid.New(), in.CampaignID, in.Title, in.Description, in.MinAmount, in.QuantityLimit).
		Scan(&t.ID, &t.CampaignID, &t.Title, &t.Description, &t.MinAmount, &t.QuantityLimit, &t.ClaimedCount)
	return &t, err
}

func (s *Store) Tiers(ctx context.Context, campaignID uuid.UUID) ([]Tier, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, campaign_id, title, COALESCE(description, ''), min_amount, quantity_limit, claimed_count
		  FROM reward_tiers WHERE campaign_id = $1 ORDER BY min_amount ASC`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Tier
	for rows.Next() {
		var t Tier
		if err := rows.Scan(&t.ID, &t.CampaignID, &t.Title, &t.Description, &t.MinAmount, &t.QuantityLimit, &t.ClaimedCount); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

var ErrNotFound = errors.New("campaign not found")
