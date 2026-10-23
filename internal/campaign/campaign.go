package campaign

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/maczeo11/cinefund/internal/platform/errs"
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
	CoverKey     string     `json:"cover_key,omitempty"`
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
	CoverKey  string    `json:"cover_key,omitempty"`
}

type NewTier struct {
	CampaignID    uuid.UUID `json:"-"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	MinAmount     int64     `json:"min_amount"`
	QuantityLimit *int      `json:"quantity_limit"`
}

// Categories mirrors the CHECK constraint on campaigns.category.
var Categories = map[string]bool{
	"DRAMA": true, "COMEDY": true, "DOCUMENTARY": true, "ANIMATION": true,
	"HORROR": true, "SCIFI": true, "EXPERIMENTAL": true,
}

// MinGoalPaise mirrors campaigns.goal_amount >= 100000 (INR 1,000).
const MinGoalPaise = 100_000

// MinTierPaise mirrors reward_tiers.min_amount >= 10000 (INR 100).
const MinTierPaise = 10_000

// Validate checks a new campaign against the same rules the schema enforces,
// so a bad request becomes a 400 with a reason instead of a constraint
// violation surfacing as a 500. The database stays the source of truth.
func (n *NewCampaign) Validate() error {
	n.Title = strings.TrimSpace(n.Title)
	n.Tagline = strings.TrimSpace(n.Tagline)
	n.Category = strings.ToUpper(strings.TrimSpace(n.Category))
	n.CoverKey = strings.TrimSpace(n.CoverKey)

	if l := utf8.RuneCountInString(n.Title); l < 3 || l > 120 {
		return errs.Invalid("INVALID_TITLE", "title must be 3-120 characters")
	}
	if utf8.RuneCountInString(n.Tagline) > 200 {
		return errs.Invalid("INVALID_TAGLINE", "tagline must be at most 200 characters")
	}
	if utf8.RuneCountInString(n.Synopsis) > 10_000 {
		return errs.Invalid("INVALID_SYNOPSIS", "synopsis must be at most 10000 characters")
	}
	if !Categories[n.Category] {
		return errs.Invalid("INVALID_CATEGORY", "unknown category %q", n.Category)
	}
	if n.Goal < MinGoalPaise {
		return errs.Invalid("INVALID_GOAL", "goal must be at least %d paise", MinGoalPaise)
	}
	if n.CoverKey != "" {
		// cover_key is rendered as an <img src> for every visitor, so only
		// accept absolute https URLs: no javascript:, data:, or plain http.
		u, err := url.Parse(n.CoverKey)
		if err != nil || u.Scheme != "https" || u.Host == "" || len(n.CoverKey) > 2048 {
			return errs.Invalid("INVALID_COVER", "cover must be an https URL")
		}
	}
	return nil
}

// Validate mirrors the reward_tiers CHECK constraints.
func (t *NewTier) Validate() error {
	t.Title = strings.TrimSpace(t.Title)
	if l := utf8.RuneCountInString(t.Title); l < 2 || l > 80 {
		return errs.Invalid("INVALID_TIER_TITLE", "tier title must be 2-80 characters")
	}
	if utf8.RuneCountInString(t.Description) > 2000 {
		return errs.Invalid("INVALID_TIER_DESCRIPTION", "tier description must be at most 2000 characters")
	}
	if t.MinAmount < MinTierPaise {
		return errs.Invalid("INVALID_TIER_AMOUNT", "tier minimum must be at least %d paise", MinTierPaise)
	}
	if t.QuantityLimit != nil && *t.QuantityLimit <= 0 {
		return errs.Invalid("INVALID_TIER_LIMIT", "quantity limit must be positive")
	}
	return nil
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Create(ctx context.Context, in NewCampaign) (*Campaign, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO campaigns (id, creator_id, slug, title, tagline, synopsis,
		                       category, goal_amount, cover_key, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'DRAFT')
		RETURNING id`,
		uuid.New(), in.CreatorID, uuid.NewString()[:8], in.Title, in.Tagline,
		in.Synopsis, in.Category, in.Goal, in.CoverKey).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Store) Get(ctx context.Context, id uuid.UUID) (*Campaign, error) {
	var c Campaign
	err := s.pool.QueryRow(ctx, `
		SELECT id, creator_id, title, tagline, COALESCE(synopsis, ''), category,
		       goal_amount, raised_amount, backer_count, status, deadline, COALESCE(cover_key, '')
		  FROM campaigns WHERE id = $1`, id).
		Scan(&c.ID, &c.CreatorID, &c.Title, &c.Tagline, &c.Synopsis, &c.Category,
			&c.GoalAmount, &c.RaisedAmount, &c.BackerCount, &c.Status, &c.Deadline, &c.CoverKey)
	return &c, err
}

func (s *Store) List(ctx context.Context) ([]Campaign, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, creator_id, title, tagline, COALESCE(synopsis, ''), category,
		       goal_amount, raised_amount, backer_count, status, deadline, COALESCE(cover_key, '')
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
			&c.GoalAmount, &c.RaisedAmount, &c.BackerCount, &c.Status, &c.Deadline, &c.CoverKey); err != nil {
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
