package campaign

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/maczeo11/cinefund/internal/platform/httpx"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestCampaign_Create_Unauthenticated(t *testing.T) {
	h := NewHandler(nil)
	r := gin.New()
	r.POST("/campaigns", h.Create)

	body := NewCampaign{
		Title: "My Film",
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/campaigns", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCampaign_Create_CreatorMismatch(t *testing.T) {
	h := NewHandler(nil)
	callerID := uuid.New()
	otherID := uuid.New()

	r := gin.New()
	r.POST("/campaigns", func(c *gin.Context) {
		httpx.SetCallerID(c, callerID)
		h.Create(c)
	})

	body := NewCampaign{
		CreatorID: otherID,
		Title:     "My Film",
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/campaigns", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for creator mismatch, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCampaign_SetLive_Unauthenticated(t *testing.T) {
	h := NewHandler(nil)
	r := gin.New()
	r.POST("/campaigns/:id/publish", h.SetLive)

	req := httptest.NewRequest("POST", "/campaigns/"+uuid.NewString()+"/publish", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCampaign_SetLive_InvalidID(t *testing.T) {
	h := NewHandler(nil)
	r := gin.New()
	r.POST("/campaigns/:id/publish", h.SetLive)

	req := httptest.NewRequest("POST", "/campaigns/not-a-uuid/publish", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for invalid uuid, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCampaign_AddTier_Unauthenticated(t *testing.T) {
	h := NewHandler(nil)
	r := gin.New()
	r.POST("/campaigns/:id/tiers", h.AddTier)

	req := httptest.NewRequest("POST", "/campaigns/"+uuid.NewString()+"/tiers", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCampaign_AddTier_InvalidID(t *testing.T) {
	h := NewHandler(nil)
	r := gin.New()
	r.POST("/campaigns/:id/tiers", h.AddTier)

	req := httptest.NewRequest("POST", "/campaigns/not-a-uuid/tiers", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for invalid uuid, got %d: %s", w.Code, w.Body.String())
	}
}

type fakeCampaignStore struct {
	campaigns map[uuid.UUID]*Campaign
	tiers     map[uuid.UUID][]Tier
}

func newFakeCampaignStore() *fakeCampaignStore {
	return &fakeCampaignStore{
		campaigns: make(map[uuid.UUID]*Campaign),
		tiers:     make(map[uuid.UUID][]Tier),
	}
}

func (f *fakeCampaignStore) Create(_ context.Context, in NewCampaign) (*Campaign, error) {
	id := uuid.New()
	c := &Campaign{
		ID:         id,
		CreatorID:  in.CreatorID,
		Title:      in.Title,
		Tagline:    in.Tagline,
		Synopsis:   in.Synopsis,
		Category:   in.Category,
		GoalAmount: in.Goal,
		Status:     "DRAFT",
	}
	f.campaigns[id] = c
	return c, nil
}

func (f *fakeCampaignStore) Get(_ context.Context, id uuid.UUID) (*Campaign, error) {
	c, ok := f.campaigns[id]
	if !ok {
		return nil, ErrNotFound
	}
	return c, nil
}

func (f *fakeCampaignStore) List(_ context.Context) ([]Campaign, error) {
	var out []Campaign
	for _, c := range f.campaigns {
		out = append(out, *c)
	}
	return out, nil
}

func (f *fakeCampaignStore) SetLive(_ context.Context, id uuid.UUID) error {
	c, ok := f.campaigns[id]
	if !ok {
		return ErrNotFound
	}
	c.Status = "LIVE"
	return nil
}

func (f *fakeCampaignStore) AddTier(_ context.Context, in NewTier) (*Tier, error) {
	t := &Tier{
		ID:          uuid.New(),
		CampaignID:  in.CampaignID,
		Title:       in.Title,
		Description: in.Description,
		MinAmount:   in.MinAmount,
	}
	f.tiers[in.CampaignID] = append(f.tiers[in.CampaignID], *t)
	return t, nil
}

func (f *fakeCampaignStore) Tiers(_ context.Context, campaignID uuid.UUID) ([]Tier, error) {
	return f.tiers[campaignID], nil
}

func TestCampaign_Create_Success_DefaultsToCallerID(t *testing.T) {
	store := newFakeCampaignStore()
	h := NewHandler(store)
	callerID := uuid.New()

	r := gin.New()
	r.POST("/campaigns", func(c *gin.Context) {
		httpx.SetCallerID(c, callerID)
		h.Create(c)
	})

	body := NewCampaign{
		Title: "Sci-Fi Epic",
		Goal:  500000,
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/campaigns", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}
	var res Campaign
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.CreatorID != callerID {
		t.Errorf("expected CreatorID to be %s, got %s", callerID, res.CreatorID)
	}
}

func TestCampaign_SetLive_OwnershipCheck(t *testing.T) {
	store := newFakeCampaignStore()
	h := NewHandler(store)

	creatorID := uuid.New()
	attackerID := uuid.New()

	c, _ := store.Create(nil, NewCampaign{CreatorID: creatorID, Title: "Indie Doc"})

	r := gin.New()
	r.POST("/campaigns/:id/publish", func(c *gin.Context) {
		role := c.GetHeader("X-Role")
		if role == "attacker" {
			httpx.SetCallerID(c, attackerID)
		} else {
			httpx.SetCallerID(c, creatorID)
		}
		h.SetLive(c)
	})

	// 1. Attacker tries to publish
	req := httptest.NewRequest("POST", "/campaigns/"+c.ID.String()+"/publish", nil)
	req.Header.Set("X-Role", "attacker")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for non-owner, got %d: %s", w.Code, w.Body.String())
	}

	// 2. Non-existent campaign
	req2 := httptest.NewRequest("POST", "/campaigns/"+uuid.NewString()+"/publish", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d: %s", w2.Code, w2.Body.String())
	}

	// 3. Creator publishes own campaign
	req3 := httptest.NewRequest("POST", "/campaigns/"+c.ID.String()+"/publish", nil)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for creator, got %d: %s", w3.Code, w3.Body.String())
	}
	if c.Status != "LIVE" {
		t.Errorf("expected campaign status to be LIVE, got %s", c.Status)
	}
}

func TestCampaign_AddTier_OwnershipCheck(t *testing.T) {
	store := newFakeCampaignStore()
	h := NewHandler(store)

	creatorID := uuid.New()
	attackerID := uuid.New()

	c, _ := store.Create(nil, NewCampaign{CreatorID: creatorID, Title: "Animation Short"})

	r := gin.New()
	r.POST("/campaigns/:id/tiers", func(ctx *gin.Context) {
		role := ctx.GetHeader("X-Role")
		if role == "attacker" {
			httpx.SetCallerID(ctx, attackerID)
		} else {
			httpx.SetCallerID(ctx, creatorID)
		}
		h.AddTier(ctx)
	})

	tierBody := NewTier{
		Title:     "Early Bird",
		MinAmount: 1000,
	}
	b, _ := json.Marshal(tierBody)

	// 1. Attacker tries to add tier
	req := httptest.NewRequest("POST", "/campaigns/"+c.ID.String()+"/tiers", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Role", "attacker")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for non-owner, got %d: %s", w.Code, w.Body.String())
	}

	// 2. Non-existent campaign
	req2 := httptest.NewRequest("POST", "/campaigns/"+uuid.NewString()+"/tiers", bytes.NewReader(b))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d: %s", w2.Code, w2.Body.String())
	}

	// 3. Creator adds tier
	req3 := httptest.NewRequest("POST", "/campaigns/"+c.ID.String()+"/tiers", bytes.NewReader(b))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for creator, got %d: %s", w3.Code, w3.Body.String())
	}
}
