package media

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/maczeo11/cinefund/internal/media/transcode"
	"github.com/maczeo11/cinefund/internal/platform/errs"
	"github.com/maczeo11/cinefund/internal/platform/httpx"
)

// SegmentURLTTL bounds how long a copied segment URL stays usable.
//
// The player fetches playlists through the API (auth required) while video
// bytes stream straight from storage over presigned URLs. A viewer who copies
// a segment URL out of DevTools gets a link that dies within minutes instead
// of a permanent download.
const SegmentURLTTL = 10 * time.Minute

const playlistMIME = "application/vnd.apple.mpegurl"

// maxPlaylistBytes caps how much playlist text is read from storage.
const maxPlaylistBytes = 1 << 20 // 1MB

// rungNamePattern whitelists rendition names in URLs. Rung names are
// server-generated (e.g. "720p"), so anything outside this set is rejected
// before it can become a storage key.
var rungNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// PlaybackStore captures the persistence + storage methods required by
// PlaybackHandler.
type PlaybackStore interface {
	GetPlaybackAsset(ctx context.Context, id uuid.UUID) (*PlaybackAsset, error)
	LatestReadyAssetByCampaign(ctx context.Context, campaignID uuid.UUID) (*PlaybackAsset, error)
	GetObject(ctx context.Context, key string) (io.ReadCloser, error)
	PresignPublicGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// PlaybackHandler serves HLS playlists for READY assets.
//
// The bucket stays private: the API serves only playlists (a few KB of text,
// auth required, Cache-Control: no-store) and rewrites every segment
// reference into a short-lived presigned URL, so video bytes still never pass
// through Go. This is the seam where HLS AES-128 key delivery plugs in later
// (a key endpoint + #EXT-X-KEY lines in the served variants).
type PlaybackHandler struct {
	store      PlaybackStore
	segmentTTL time.Duration
}

func NewPlaybackHandler(store PlaybackStore) *PlaybackHandler {
	return &PlaybackHandler{store: store, segmentTTL: SegmentURLTTL}
}

// CampaignVideo handles GET /campaigns/:id/video (public).
//
// It reveals only playback metadata - the playlists themselves stay behind
// auth - so anonymous visitors can still see whether a campaign has a
// watchable reel.
func (h *PlaybackHandler) CampaignVideo(c *gin.Context) {
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Abort(c, errs.Invalid("INVALID_ID", "campaign id is not a uuid"))
		return
	}
	asset, err := h.store.LatestReadyAssetByCampaign(c.Request.Context(), campaignID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	if asset == nil {
		httpx.OK(c, gin.H{"asset_id": nil, "status": "NONE"})
		return
	}
	httpx.OK(c, gin.H{
		"asset_id":   asset.ID,
		"status":     "READY",
		"master_url": "/api/v1/videos/" + asset.ID.String() + "/master.m3u8",
	})
}

// Master handles GET /videos/:id/master.m3u8 (auth required).
//
// The stored master references variants by relative path
// ("<rung>/index.m3u8"). Those are rewritten to the API variant endpoints so
// every subsequent playlist fetch also passes auth; segments are signed later
// at the variant level, keeping each presigned URL as short-lived as possible.
func (h *PlaybackHandler) Master(c *gin.Context) {
	if !requirePlaybackCaller(c) {
		return
	}
	asset, ok := h.loadReadyAsset(c)
	if !ok {
		return
	}
	raw, err := readPlaylist(c.Request.Context(), h.store, asset.MasterKey)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out, err := rewriteMasterRefs(raw, asset)
	if err != nil {
		_ = c.Error(err)
		return
	}
	servePlaylist(c, out)
}

// Variant handles GET /videos/:id/variants/:rung/index.m3u8 (auth required).
//
// The stored variant references bare segment filenames; each is replaced with
// a short-lived presigned URL so the bucket can stay private.
func (h *PlaybackHandler) Variant(c *gin.Context) {
	if !requirePlaybackCaller(c) {
		return
	}
	asset, ok := h.loadReadyAsset(c)
	if !ok {
		return
	}
	rung := c.Param("rung")
	if !rungNamePattern.MatchString(rung) {
		httpx.Abort(c, errs.Invalid("INVALID_RUNG", "unknown rendition"))
		return
	}
	var rungKey string
	for _, r := range asset.Rungs {
		if r.Name == rung {
			rungKey = r.Key
			break
		}
	}
	if rungKey == "" {
		httpx.Abort(c, errs.NotFound("RENDITION_NOT_FOUND", "rendition not found"))
		return
	}
	raw, err := readPlaylist(c.Request.Context(), h.store, rungKey)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out, err := transcode.RewriteVariantPlaylist(raw, asset.ID.String(), asset.PipelineVersion, rung, playbackPresigner{
		ctx:   c.Request.Context(),
		store: h.store,
		ttl:   h.segmentTTL,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}
	servePlaylist(c, out)
}

// requirePlaybackCaller enforces auth on playlist endpoints, mirroring the
// upload handlers. The routes are additionally registered behind RequireAuth;
// this keeps the handler safe when unit-tested or re-mounted.
func requirePlaybackCaller(c *gin.Context) bool {
	if _, ok := httpx.CallerID(c); !ok {
		httpx.Abort(c, errs.Unauthorized("UNAUTHORIZED", "authentication required"))
		return false
	}
	return true
}

// loadReadyAsset resolves :id to a playable asset.
func (h *PlaybackHandler) loadReadyAsset(c *gin.Context) (*PlaybackAsset, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Abort(c, errs.Invalid("INVALID_ID", "asset id is not a uuid"))
		return nil, false
	}
	asset, err := h.store.GetPlaybackAsset(c.Request.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ErrAssetNotFound) {
		httpx.Abort(c, errs.NotFound("ASSET_NOT_FOUND", "asset not found"))
		return nil, false
	}
	if err != nil {
		_ = c.Error(err)
		return nil, false
	}
	if asset.Status != "READY" || asset.MasterKey == "" {
		httpx.Abort(c, errs.Conflict("VIDEO_NOT_READY", "video is not ready for playback (status %s)", asset.Status))
		return nil, false
	}
	return asset, true
}

// rewriteMasterRefs maps stored variant references ("<rung>/index.m3u8") to
// API-relative endpoints ("variants/<rung>/index.m3u8"). Only rungs recorded
// on the asset are accepted: a master playlist referencing anything else is
// treated as corrupt rather than served.
func rewriteMasterRefs(raw []byte, asset *PlaybackAsset) ([]byte, error) {
	known := make(map[string]bool, len(asset.Rungs))
	for _, r := range asset.Rungs {
		known[r.Name] = true
	}
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}
		rung, file, ok := strings.Cut(line, "/")
		if !ok || file != "index.m3u8" || !rungNamePattern.MatchString(rung) || !known[rung] {
			return nil, fmt.Errorf("unexpected variant reference in master playlist: %q", line)
		}
		out.WriteString("variants/" + rung + "/index.m3u8\n")
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// readPlaylist fetches one playlist object from private storage.
func readPlaylist(ctx context.Context, store PlaybackStore, key string) ([]byte, error) {
	rc, err := store.GetObject(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	raw, err := io.ReadAll(io.LimitReader(rc, maxPlaylistBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxPlaylistBytes {
		return nil, fmt.Errorf("playlist %q exceeds size limit", key)
	}
	return raw, nil
}

// servePlaylist writes a playlist with no-store semantics: variant fetches
// re-sign segments each time instead of replaying stale signed URLs.
func servePlaylist(c *gin.Context, body []byte) {
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, playlistMIME, body)
}

// playbackPresigner adapts PlaybackStore to transcode.Presigner with the
// short segment TTL.
type playbackPresigner struct {
	ctx   context.Context
	store PlaybackStore
	ttl   time.Duration
}

func (p playbackPresigner) PresignedGet(key string) (string, error) {
	return p.store.PresignPublicGet(p.ctx, key, p.ttl)
}
