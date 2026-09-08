package media

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/maczeo11/cinefund/internal/media/transcode"
	"github.com/maczeo11/cinefund/internal/platform/errs"
	"github.com/maczeo11/cinefund/internal/platform/httpx"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestUploadConstantsAndMIMETypes(t *testing.T) {
	// 500MB cap
	const expectedCap int64 = 500 * 1024 * 1024
	if MaxUploadBytes != expectedCap {
		t.Fatalf("MaxUploadBytes = %d, want %d", MaxUploadBytes, expectedCap)
	}

	// Required allowed MIME types
	requiredTypes := []string{
		"video/mp4",
		"video/quicktime",
		"video/webm",
		"video/x-matroska",
		"text/plain", // test
	}
	for _, ct := range requiredTypes {
		if !AllowedVideoMIMETypes[ct] {
			t.Errorf("expected %s to be an allowed video MIME type", ct)
		}
	}

	// Disallowed types
	disallowedTypes := []string{
		"image/png",
		"image/jpeg",
		"application/octet-stream",
		"text/html",
		"application/x-sh",
	}
	for _, dt := range disallowedTypes {
		if AllowedVideoMIMETypes[dt] {
			t.Errorf("expected %s to be disallowed", dt)
		}
	}
}

func TestPresign_Unauthenticated(t *testing.T) {
	h := NewHandler(nil, nil)
	r := gin.New()
	r.POST("/uploads", h.Presign)

	body := map[string]any{
		"content_type": "video/mp4",
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/uploads", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPresign_DisallowedMIME(t *testing.T) {
	h := NewHandler(nil, nil)
	callerID := uuid.New()

	r := gin.New()
	r.POST("/uploads", func(c *gin.Context) {
		httpx.SetCallerID(c, callerID)
		h.Presign(c)
	})

	body := map[string]any{
		"owner_id":     callerID.String(),
		"content_type": "application/x-executable",
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/uploads", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for invalid mime, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPresign_ExceedsSizeCap(t *testing.T) {
	h := NewHandler(nil, nil)
	callerID := uuid.New()

	r := gin.New()
	r.POST("/uploads", func(c *gin.Context) {
		httpx.SetCallerID(c, callerID)
		h.Presign(c)
	})

	oversized := MaxUploadBytes + 1024
	body := map[string]any{
		"owner_id":     callerID.String(),
		"content_type": "video/mp4",
		"size_bytes":   oversized,
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/uploads", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for oversized payload, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPresign_OwnerMismatch(t *testing.T) {
	h := NewHandler(nil, nil)
	callerID := uuid.New()
	otherID := uuid.New()

	r := gin.New()
	r.POST("/uploads", func(c *gin.Context) {
		httpx.SetCallerID(c, callerID)
		h.Presign(c)
	})

	body := map[string]any{
		"owner_id":     otherID.String(),
		"content_type": "video/mp4",
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/uploads", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for owner mismatch, got %d: %s", w.Code, w.Body.String())
	}
}

func TestComplete_Unauthenticated(t *testing.T) {
	h := NewHandler(nil, nil)
	r := gin.New()
	r.POST("/uploads/:id/complete", h.Complete)

	req := httptest.NewRequest("POST", "/uploads/"+uuid.NewString()+"/complete", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d: %s", w.Code, w.Body.String())
	}
}

func TestComplete_InvalidID(t *testing.T) {
	h := NewHandler(nil, nil)
	r := gin.New()
	r.POST("/uploads/:id/complete", h.Complete)

	req := httptest.NewRequest("POST", "/uploads/not-a-uuid/complete", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for invalid uuid, got %d: %s", w.Code, w.Body.String())
	}
}

type fakeMediaStore struct {
	campaignOwners map[uuid.UUID]uuid.UUID // campaignID -> ownerID
	assets         map[uuid.UUID]*Asset
	failMarkUpload error
}

func newFakeMediaStore() *fakeMediaStore {
	return &fakeMediaStore{
		campaignOwners: make(map[uuid.UUID]uuid.UUID),
		assets:         make(map[uuid.UUID]*Asset),
	}
}

func (f *fakeMediaStore) CheckCampaignOwner(_ context.Context, campaignID, ownerID uuid.UUID) (bool, error) {
	creator, ok := f.campaignOwners[campaignID]
	if !ok {
		return false, pgx.ErrNoRows
	}
	return creator == ownerID, nil
}

func (f *fakeMediaStore) CreateAsset(_ context.Context, ownerID uuid.UUID, campaignID *uuid.UUID, purpose transcode.Purpose, contentType string) (*Asset, error) {
	id := uuid.New()
	a := &Asset{
		ID:          id,
		OwnerID:     ownerID,
		CampaignID:  campaignID,
		Purpose:     purpose,
		StorageKey:  "uploads/" + id.String(),
		ContentType: contentType,
		Status:      "PENDING_UPLOAD",
	}
	f.assets[id] = a
	return a, nil
}

func (f *fakeMediaStore) PresignPut(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://s3.example.com/" + key + "?signed=true", nil
}

func (f *fakeMediaStore) Get(_ context.Context, id uuid.UUID) (*Asset, error) {
	a, ok := f.assets[id]
	if !ok {
		return nil, ErrAssetNotFound
	}
	return a, nil
}

func (f *fakeMediaStore) MarkUploaded(_ context.Context, assetID uuid.UUID) error {
	if f.failMarkUpload != nil {
		return f.failMarkUpload
	}
	a, ok := f.assets[assetID]
	if !ok {
		return ErrAssetNotFound
	}
	a.Status = "UPLOADED"
	return nil
}

type fakeJobRepo struct {
	enqueued []uuid.UUID
}

func (f *fakeJobRepo) Enqueue(_ context.Context, assetID uuid.UUID, _ int) (uuid.UUID, error) {
	f.enqueued = append(f.enqueued, assetID)
	return uuid.New(), nil
}

func TestPresign_MIMENormalization(t *testing.T) {
	store := newFakeMediaStore()
	repo := &fakeJobRepo{}
	h := NewHandler(store, repo)
	callerID := uuid.New()

	r := gin.New()
	r.POST("/uploads", func(c *gin.Context) {
		httpx.SetCallerID(c, callerID)
		h.Presign(c)
	})

	// video/mp4 with codecs parameter and mixed case
	body := map[string]any{
		"owner_id":     callerID.String(),
		"content_type": "VIDEO/MP4; codecs=\"avc1.42E01E\"",
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/uploads", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for normalized mime, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPresign_NegativeOrZeroSize(t *testing.T) {
	store := newFakeMediaStore()
	repo := &fakeJobRepo{}
	h := NewHandler(store, repo)
	callerID := uuid.New()

	r := gin.New()
	r.POST("/uploads", func(c *gin.Context) {
		httpx.SetCallerID(c, callerID)
		h.Presign(c)
	})

	for _, invalidSize := range []int64{0, -1, -500} {
		body := map[string]any{
			"owner_id":     callerID.String(),
			"content_type": "video/mp4",
			"size_bytes":   invalidSize,
		}
		b, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/uploads", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for size %d, got %d", invalidSize, w.Code)
		}
	}
}

func TestPresign_CampaignOwnershipCheck(t *testing.T) {
	store := newFakeMediaStore()
	repo := &fakeJobRepo{}
	h := NewHandler(store, repo)

	creatorID := uuid.New()
	attackerID := uuid.New()
	campaignID := uuid.New()
	store.campaignOwners[campaignID] = creatorID

	r := gin.New()
	r.POST("/uploads", func(c *gin.Context) {
		role := c.GetHeader("X-Role")
		if role == "attacker" {
			httpx.SetCallerID(c, attackerID)
		} else {
			httpx.SetCallerID(c, creatorID)
		}
		h.Presign(c)
	})

	// 1. Attacker tries to upload for creator's campaign
	bodyAttacker := map[string]any{
		"owner_id":     attackerID.String(),
		"campaign_id":  campaignID.String(),
		"content_type": "video/mp4",
	}
	b, _ := json.Marshal(bodyAttacker)
	req := httptest.NewRequest("POST", "/uploads", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Role", "attacker")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for caller not owning campaign, got %d: %s", w.Code, w.Body.String())
	}

	// 2. Non-existent campaign
	bodyNotFound := map[string]any{
		"owner_id":     creatorID.String(),
		"campaign_id":  uuid.NewString(),
		"content_type": "video/mp4",
	}
	b2, _ := json.Marshal(bodyNotFound)
	req2 := httptest.NewRequest("POST", "/uploads", bytes.NewReader(b2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found for non-existent campaign, got %d: %s", w2.Code, w2.Body.String())
	}

	// 3. Creator uploads for own campaign
	bodyCreator := map[string]any{
		"owner_id":     creatorID.String(),
		"campaign_id":  campaignID.String(),
		"content_type": "video/mp4",
	}
	b3, _ := json.Marshal(bodyCreator)
	req3 := httptest.NewRequest("POST", "/uploads", bytes.NewReader(b3))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)

	if w3.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for campaign owner, got %d: %s", w3.Code, w3.Body.String())
	}
}

func TestComplete_OwnershipCheck(t *testing.T) {
	store := newFakeMediaStore()
	repo := &fakeJobRepo{}
	h := NewHandler(store, repo)

	creatorID := uuid.New()
	attackerID := uuid.New()

	asset, _ := store.CreateAsset(context.Background(), creatorID, nil, transcode.PurposeFilm, "video/mp4")

	r := gin.New()
	r.POST("/uploads/:id/complete", func(c *gin.Context) {
		role := c.GetHeader("X-Role")
		if role == "attacker" {
			httpx.SetCallerID(c, attackerID)
		} else {
			httpx.SetCallerID(c, creatorID)
		}
		h.Complete(c)
	})

	// 1. Attacker tries to complete creator's asset
	req := httptest.NewRequest("POST", "/uploads/"+asset.ID.String()+"/complete", nil)
	req.Header.Set("X-Role", "attacker")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for non-owner, got %d: %s", w.Code, w.Body.String())
	}

	// 2. Non-existent asset
	req2 := httptest.NewRequest("POST", "/uploads/"+uuid.NewString()+"/complete", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found for missing asset, got %d: %s", w2.Code, w2.Body.String())
	}

	// 3. Creator completes own asset
	req3 := httptest.NewRequest("POST", "/uploads/"+asset.ID.String()+"/complete", nil)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for owner, got %d: %s", w3.Code, w3.Body.String())
	}
	if len(repo.enqueued) != 1 || repo.enqueued[0] != asset.ID {
		t.Errorf("expected asset to be enqueued for transcoding, got %v", repo.enqueued)
	}

	// 4. Object exceeded size cap when uploaded
	store.failMarkUpload = errs.Invalid("FILE_TOO_LARGE", "uploaded object size exceeds maximum 500MB")
	asset2, _ := store.CreateAsset(context.Background(), creatorID, nil, transcode.PurposeFilm, "video/mp4")
	req4 := httptest.NewRequest("POST", "/uploads/"+asset2.ID.String()+"/complete", nil)
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for oversized upload, got %d: %s", w4.Code, w4.Body.String())
	}
}
