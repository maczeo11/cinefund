package pledge

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/maczeo11/cinefund/internal/platform/httpx"
)

// pledgeRouter mounts CreatePledge, authenticating as caller when it is set.
func pledgeRouter(svc *Service, caller uuid.UUID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(httpx.Middleware(slog.New(slog.NewTextHandler(io.Discard, nil))))
	r.Use(func(c *gin.Context) {
		if caller != uuid.Nil {
			httpx.SetCallerID(c, caller)
		}
		c.Next()
	})
	r.POST("/campaigns/:id/pledges", NewHandler(svc).CreatePledge)
	return r
}

func postPledge(r *gin.Engine, campaignID uuid.UUID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/campaigns/"+campaignID.String()+"/pledges", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCreatePledgeHandler_RequiresCaller(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	svc, _, _ := testService(fq)

	w := postPledge(pledgeRouter(svc, uuid.Nil), c.ID,
		`{"backer_id":"`+uuid.NewString()+`","amount":100000}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a caller, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreatePledgeHandler_BackerIsCallerNotBody(t *testing.T) {
	fq := newFakeQueries()
	c := liveCampaign(uuid.New(), time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	svc, _, _ := testService(fq)
	caller := uuid.New()

	w := postPledge(pledgeRouter(svc, caller), c.ID,
		`{"backer_id":"`+uuid.NewString()+`","amount":100000}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := fq.Pledge(resp.ID).BackerID; got != caller {
		t.Fatalf("pledge backer = %s, want the caller %s", got, caller)
	}
}

// Naming someone else as backer used to sidestep the creator check.
func TestCreatePledgeHandler_CreatorCannotPledgeViaBodyBackerID(t *testing.T) {
	fq := newFakeQueries()
	creator := uuid.New()
	c := liveCampaign(creator, time.Now().Add(24*time.Hour))
	fq.seedCampaign(c)
	svc, _, _ := testService(fq)

	w := postPledge(pledgeRouter(svc, creator), c.ID,
		`{"backer_id":"`+uuid.NewString()+`","amount":100000}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for the creator, got %d: %s", w.Code, w.Body.String())
	}
}
