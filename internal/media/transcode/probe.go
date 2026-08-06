package transcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// RejectReason is a stable, machine-readable rejection code. These are stored on
// media_assets.reject_reason and surfaced to the uploader.
type RejectReason string

const (
	ReasonNoVideoStream    RejectReason = "no_video_stream"
	ReasonBadDuration      RejectReason = "undeterminable_duration"
	ReasonTooLong          RejectReason = "too_long"
	ReasonPitchTooLong     RejectReason = "pitch_too_long"
	ReasonResolutionTooLow RejectReason = "resolution_too_low"
	ReasonUnsupportedCodec RejectReason = "unsupported_codec"
	ReasonTypeMismatch     RejectReason = "content_type_mismatch"
)

// RejectError carries a rejection reason. A rejected asset is a terminal,
// non-retryable outcome: retrying a file with no video stream will never
// succeed, so it must not go back on the queue.
type RejectError struct {
	Reason RejectReason
	Detail string
}

func (e *RejectError) Error() string {
	if e.Detail == "" {
		return string(e.Reason)
	}
	return fmt.Sprintf("%s: %s", e.Reason, e.Detail)
}

func reject(r RejectReason, format string, a ...any) *RejectError {
	return &RejectError{Reason: r, Detail: fmt.Sprintf(format, a...)}
}

// Purpose mirrors media_assets.purpose and sets the duration ceiling.
type Purpose string

const (
	PurposePitch   Purpose = "PITCH"
	PurposeFilm    Purpose = "FILM"
	PurposeTrailer Purpose = "TRAILER"
	PurposeBTS     Purpose = "BTS"
)

const (
	maxFilmSeconds  = 3600 // 1 hour
	maxPitchSeconds = 300  // 5 minutes
	minWidth        = 640
	minHeight       = 360
)

// decodableVideo is the codec allow-list. Anything outside it is rejected before
// a single FFmpeg process starts, because discovering an undecodable stream 40
// minutes into an encode wastes the whole 40 minutes.
var decodableVideo = map[string]bool{
	"h264": true, "hevc": true, "vp8": true, "vp9": true, "av1": true,
	"mpeg4": true, "mpeg2video": true, "prores": true, "dnxhd": true,
	"theora": true, "wmv3": true, "vc1": true,
}

// Probe is the parsed subset of ffprobe output the pipeline reasons about. The
// raw JSON is kept alongside it and stored verbatim.
type Probe struct {
	Raw json.RawMessage

	DurationSecs float64
	SizeBytes    int64
	FormatName   string

	HasVideo   bool
	VideoCodec string
	Width      int
	Height     int
	Rotation   int
	PixFmt     string
	FrameRate  float64

	HasAudio   bool
	AudioCodec string
}

// DisplayDimensions returns the dimensions after the rotation matrix is applied.
func (p Probe) DisplayDimensions() (int, int) {
	return DisplayDimensions(p.Width, p.Height, p.Rotation)
}

// ---------------------------------------------------------------------------
// ffprobe output shapes
// ---------------------------------------------------------------------------

type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

type ffprobeFormat struct {
	Duration   string `json:"duration"`
	Size       string `json:"size"`
	FormatName string `json:"format_name"`
}

type ffprobeStream struct {
	CodecType  string `json:"codec_type"`
	CodecName  string `json:"codec_name"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	PixFmt     string `json:"pix_fmt"`
	RFrameRate string `json:"r_frame_rate"`
	Duration   string `json:"duration"`
	Tags       struct {
		Rotate string `json:"rotate"`
	} `json:"tags"`
	SideData []struct {
		Type     string   `json:"side_data_type"`
		Rotation *float64 `json:"rotation"`
	} `json:"side_data_list"`
}

// Prober runs ffprobe.
type Prober struct{ Path string }

func NewProber(path string) *Prober {
	if path == "" {
		path = "ffprobe"
	}
	return &Prober{Path: path}
}

// Run probes the input, which is normally a presigned GET URL.
func (p *Prober) Run(ctx context.Context, input string) (*Probe, error) {
	cmd := exec.CommandContext(ctx, p.Path, ProbeArgs(input)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return ParseProbe(out)
}

// ParseProbe turns raw ffprobe JSON into a Probe. Split out from Run so the
// rejection rules are testable against fixture JSON with no ffprobe present.
func ParseProbe(raw []byte) (*Probe, error) {
	var o ffprobeOutput
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, fmt.Errorf("parse ffprobe output: %w", err)
	}

	pr := &Probe{Raw: raw, FormatName: o.Format.FormatName}
	pr.DurationSecs, _ = strconv.ParseFloat(o.Format.Duration, 64)
	pr.SizeBytes, _ = strconv.ParseInt(o.Format.Size, 10, 64)

	for _, s := range o.Streams {
		switch s.CodecType {
		case "video":
			if pr.HasVideo {
				continue // first video stream wins; cover art can appear as a second
			}
			pr.HasVideo = true
			pr.VideoCodec = s.CodecName
			pr.Width, pr.Height = s.Width, s.Height
			pr.PixFmt = s.PixFmt
			pr.Rotation = parseRotation(s)
			pr.FrameRate = parseFrameRate(s.RFrameRate)
			// Some containers put duration on the stream, not the format.
			if pr.DurationSecs == 0 {
				pr.DurationSecs, _ = strconv.ParseFloat(s.Duration, 64)
			}
		case "audio":
			if !pr.HasAudio {
				pr.HasAudio = true
				pr.AudioCodec = s.CodecName
			}
		}
	}
	return pr, nil
}

// parseRotation reads the rotation from either the modern displaymatrix side
// data or the legacy `rotate` tag, and normalises both to the SAME convention:
// degrees clockwise that the stored frame must be turned to display upright.
//
// The two sources disagree by sign. A portrait phone clip carries either
// `rotate: 90` as a tag or `rotation: -90` in the display matrix - the matrix
// reports the transform it applies, which is the inverse. Negating side data is
// what makes both paths produce 90 for the same physical video, which matters
// because they both write to one media_assets.rotation column.
func parseRotation(s ffprobeStream) int {
	for _, sd := range s.SideData {
		if sd.Rotation != nil {
			return normaliseRotation(-int(*sd.Rotation))
		}
	}
	if s.Tags.Rotate != "" {
		if r, err := strconv.Atoi(s.Tags.Rotate); err == nil {
			return normaliseRotation(r)
		}
	}
	return 0
}

func normaliseRotation(r int) int {
	r %= 360
	if r < 0 {
		r += 360
	}
	switch r {
	case 90, 180, 270:
		return r
	default:
		return 0
	}
}

// parseFrameRate handles ffprobe's "24000/1001" rational form.
func parseFrameRate(s string) float64 {
	num, den, ok := strings.Cut(s, "/")
	n, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0
	}
	if !ok {
		return n
	}
	d, err := strconv.ParseFloat(den, 64)
	if err != nil || d == 0 {
		return 0
	}
	return n / d
}

// ---------------------------------------------------------------------------
// Rejection rules
// ---------------------------------------------------------------------------

// Validate applies the rejection rules from docs/09 §2. It returns a
// *RejectError for anything that must not be retried.
//
// declaredType is the Content-Type the client presigned for. Checking it against
// what the file actually is is a real security control, not pedantry: a client
// that presigned for video/mp4 and uploaded something else is either broken or
// probing you.
func (p Probe) Validate(purpose Purpose, declaredType string) error {
	if !p.HasVideo {
		return reject(ReasonNoVideoStream, "container %q has no video stream", p.FormatName)
	}
	if p.DurationSecs <= 0 {
		return reject(ReasonBadDuration, "ffprobe reported duration %v", p.DurationSecs)
	}

	switch purpose {
	case PurposePitch:
		if p.DurationSecs > maxPitchSeconds {
			return reject(ReasonPitchTooLong, "%.0fs exceeds the %ds pitch limit",
				p.DurationSecs, maxPitchSeconds)
		}
	default:
		if p.DurationSecs > maxFilmSeconds {
			return reject(ReasonTooLong, "%.0fs exceeds the %ds limit",
				p.DurationSecs, maxFilmSeconds)
		}
	}

	// Dimension checks use DISPLAY dimensions: a 1080x1920 portrait clip stored
	// as 1920x1080 with rotation=90 is perfectly valid and must not be rejected
	// for being "too narrow".
	w, h := p.DisplayDimensions()
	if w < minWidth || h < minHeight {
		return reject(ReasonResolutionTooLow, "%dx%d is below the %dx%d minimum",
			w, h, minWidth, minHeight)
	}

	if !decodableVideo[p.VideoCodec] {
		return reject(ReasonUnsupportedCodec, "codec %q is not in the decodable set", p.VideoCodec)
	}

	if declaredType != "" && !formatMatchesContentType(p.FormatName, declaredType) {
		return reject(ReasonTypeMismatch, "declared %s but the container is %q",
			declaredType, p.FormatName)
	}
	return nil
}

// contentTypeFormats maps a declared Content-Type to the ffprobe format names
// that legitimately satisfy it. ffprobe reports comma-separated lists like
// "mov,mp4,m4a,3gp,3g2,mj2", so the check is membership, not equality.
var contentTypeFormats = map[string][]string{
	"video/mp4":       {"mp4", "mov", "m4a", "isom"},
	"video/quicktime": {"mov", "mp4"},
	"video/x-matroska": {"matroska", "webm"},
	"video/webm":      {"webm", "matroska"},
	"video/mpeg":      {"mpeg", "mpegts", "mpegvideo"},
	"video/x-msvideo": {"avi"},
	"video/avi":       {"avi"},
}

func formatMatchesContentType(formatName, declaredType string) bool {
	declaredType = strings.ToLower(strings.TrimSpace(strings.Split(declaredType, ";")[0]))
	want, known := contentTypeFormats[declaredType]
	if !known {
		// An unrecognised declared type is not evidence of an attack, and the
		// codec allow-list already covers "we cannot decode this". Allow it.
		return true
	}
	have := strings.Split(strings.ToLower(formatName), ",")
	for _, h := range have {
		h = strings.TrimSpace(h)
		for _, w := range want {
			if h == w {
				return true
			}
		}
	}
	return false
}
