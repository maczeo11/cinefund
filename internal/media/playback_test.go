package media

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/maczeo11/cinefund/internal/platform/httpx"
)

// fakePlaybackStore serves canned playlists for playback handler tests.
type fakePlaybackStore struct {
	assets     map[uuid.UUID]*PlaybackAsset
	byCampaign map[uuid.UUID]*PlaybackAsset
	objects    map[string]string
}

func (f *fakePlaybackStore) GetPlaybackAsset(_ context.Context, id uuid.UUID) (*PlaybackAsset, error) {
	a, ok := f.assets[id]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return a, nil
}

func (f *fakePlaybackStore) LatestReadyAssetByCampaign(_ context.Context, campaignID uuid.UUID) (*PlaybackAsset, error) {
	return f.byCampaign[campaignID], nil
}

func (f *fakePlaybackStore) GetObject(_ context.Context, key string) (io.ReadCloser, error) {
	body, ok := f.objects[key]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

func (f *fakePlaybackStore) PresignPublicGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://cdn.test/" + key + "?sig=fake", nil
}

func readyAsset() *PlaybackAsset {
	return &PlaybackAsset{
		ID:              uuid.New(),
		Status:          "READY",
		MasterKey:       "renditions/asset-1/v1/master.m3u8",
		PipelineVersion: 1,
		Rungs: []PlaybackRung{
			{Name: "720p", Key: "renditions/asset-1/v1/720p/index.m3u8"},
			{Name: "480p", Key: "renditions/asset-1/v1/480p/index.m3u8"},
		},
	}
}

// authed upstream sets a caller the way RequireAuth would have.
func withPlaybackCaller(h func(c *gin.Context)) gin.HandlerFunc {
	caller := uuid.New()
	return func(c *gin.Context) {
		httpx.SetCallerID(c, caller)
		h(c)
	}
}

// withErrorMapping mirrors the httpx.Middleware used in production: handler
// errors recorded via c.Error become 500 responses.
func withErrorMapping() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) > 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			c.Abort()
		}
	}
}

func TestPlaybackMaster_RewritesVariantRefs(t *testing.T) {
	asset := readyAsset()
	store := &fakePlaybackStore{
		assets: map[uuid.UUID]*PlaybackAsset{asset.ID: asset},
		objects: map[string]string{
			asset.MasterKey: "#EXTM3U\n#EXT-X-VERSION:3\n" +
				"#EXT-X-STREAM-INF:BANDWIDTH=2996000,RESOLUTION=1280x720\n" +
				"720p/index.m3u8\n" +
				"#EXT-X-STREAM-INF:BANDWIDTH=1498000,RESOLUTION=854x480\n" +
				"480p/index.m3u8\n",
		},
	}
	h := NewPlaybackHandler(store)
	r := gin.New()
	r.GET("/videos/:id/master.m3u8", withPlaybackCaller(h.Master))

	req := httptest.NewRequest("GET", "/videos/"+asset.ID.String()+"/master.m3u8", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != playlistMIME {
		t.Errorf("expected Content-Type %q, got %q", playlistMIME, ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("expected Cache-Control no-store, got %q", cc)
	}
	body := w.Body.String()
	for _, want := range []string{"variants/720p/index.m3u8", "variants/480p/index.m3u8"} {
		if !strings.Contains(body, want+"\n") {
			t.Errorf("expected rewritten ref %q in body:\n%s", want, body)
		}
	}
	// No raw variant ref may survive: every non-comment line must be rewritten.
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "variants/") {
			t.Errorf("unrewritten media line %q in body:\n%s", line, body)
		}
	}
}

func TestPlaybackMaster_Unauthenticated(t *testing.T) {
	h := NewPlaybackHandler(&fakePlaybackStore{})
	r := gin.New()
	r.GET("/videos/:id/master.m3u8", h.Master)

	req := httptest.NewRequest("GET", "/videos/"+uuid.NewString()+"/master.m3u8", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPlaybackMaster_NotFound(t *testing.T) {
	h := NewPlaybackHandler(&fakePlaybackStore{assets: map[uuid.UUID]*PlaybackAsset{}})
	r := gin.New()
	r.GET("/videos/:id/master.m3u8", withPlaybackCaller(h.Master))

	req := httptest.NewRequest("GET", "/videos/"+uuid.NewString()+"/master.m3u8", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPlaybackMaster_NotReady(t *testing.T) {
	asset := readyAsset()
	asset.Status = "TRANSCODING"
	asset.MasterKey = ""
	store := &fakePlaybackStore{assets: map[uuid.UUID]*PlaybackAsset{asset.ID: asset}}
	h := NewPlaybackHandler(store)
	r := gin.New()
	r.GET("/videos/:id/master.m3u8", withPlaybackCaller(h.Master))

	req := httptest.NewRequest("GET", "/videos/"+asset.ID.String()+"/master.m3u8", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPlaybackMaster_RejectsUnknownVariantRef(t *testing.T) {
	asset := readyAsset()
	store := &fakePlaybackStore{
		assets:  map[uuid.UUID]*PlaybackAsset{asset.ID: asset},
		objects: map[string]string{asset.MasterKey: "#EXTM3U\nattacker/index.m3u8\n"},
	}
	h := NewPlaybackHandler(store)
	r := gin.New()
	r.Use(withErrorMapping())
	r.GET("/videos/:id/master.m3u8", withPlaybackCaller(h.Master))

	req := httptest.NewRequest("GET", "/videos/"+asset.ID.String()+"/master.m3u8", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for corrupt master, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPlaybackVariant_SignsSegments(t *testing.T) {
	asset := readyAsset()
	variantKey := "renditions/asset-1/v1/720p/index.m3u8"
	store := &fakePlaybackStore{
		assets: map[uuid.UUID]*PlaybackAsset{asset.ID: asset},
		objects: map[string]string{
			variantKey: "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n" +
				"#EXTINF:6.0,\nseg_00000.ts\n#EXTINF:6.0,\nseg_00001.ts\n#EXT-X-ENDLIST\n",
		},
	}
	h := NewPlaybackHandler(store)
	r := gin.New()
	r.GET("/videos/:id/variants/:rung/index.m3u8", withPlaybackCaller(h.Variant))

	req := httptest.NewRequest("GET", "/videos/"+asset.ID.String()+"/variants/720p/index.m3u8", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	wantSeg := "https://cdn.test/renditions/" + asset.ID.String() + "/v1/720p/seg_00000.ts?sig=fake"
	if !strings.Contains(body, wantSeg) {
		t.Errorf("expected signed segment URL %q in body:\n%s", wantSeg, body)
	}
	if !strings.Contains(body, "#EXTINF:6.0,") || !strings.Contains(body, "#EXT-X-ENDLIST") {
		t.Errorf("expected playlist tags preserved in body:\n%s", body)
	}
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if line == "seg_00000.ts" || line == "seg_00001.ts" {
			t.Errorf("bare segment name %q served unsigned:\n%s", line, body)
		}
	}
}

func TestPlaybackVariant_UnknownRung(t *testing.T) {
	asset := readyAsset()
	store := &fakePlaybackStore{assets: map[uuid.UUID]*PlaybackAsset{asset.ID: asset}}
	h := NewPlaybackHandler(store)
	r := gin.New()
	r.GET("/videos/:id/variants/:rung/index.m3u8", withPlaybackCaller(h.Variant))

	req := httptest.NewRequest("GET", "/videos/"+asset.ID.String()+"/variants/1080p/index.m3u8", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPlaybackVariant_InvalidRung(t *testing.T) {
	asset := readyAsset()
	store := &fakePlaybackStore{assets: map[uuid.UUID]*PlaybackAsset{asset.ID: asset}}
	h := NewPlaybackHandler(store)
	r := gin.New()
	r.GET("/videos/:id/variants/:rung/index.m3u8", withPlaybackCaller(h.Variant))

	req := httptest.NewRequest("GET", "/videos/"+asset.ID.String()+"/variants/evil.ts/index.m3u8", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPlaybackCampaignVideo_ReadyAndNone(t *testing.T) {
	asset := readyAsset()
	campaignID := uuid.New()
	emptyID := uuid.New()
	store := &fakePlaybackStore{
		byCampaign: map[uuid.UUID]*PlaybackAsset{campaignID: asset},
	}
	h := NewPlaybackHandler(store)
	r := gin.New()
	r.GET("/campaigns/:id/video", h.CampaignVideo)

	req := httptest.NewRequest("GET", "/campaigns/"+campaignID.String()+"/video", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, asset.ID.String()) || !strings.Contains(body, "/api/v1/videos/") {
		t.Errorf("expected asset id and master url in body: %s", body)
	}

	req = httptest.NewRequest("GET", "/campaigns/"+emptyID.String()+"/video", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"NONE"`) {
		t.Errorf("expected NONE status in body: %s", w.Body.String())
	}
}
