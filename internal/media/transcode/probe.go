package transcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// RejectReason is stored on media_assets.reject_reason.
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

// RejectError is terminal — no point retrying a file with no video stream.
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

// Purpose sets the duration ceiling for validation.
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

// reject unsupported codecs before spending 40 min on an encode
var decodableVideo = map[string]bool{
	"h264": true, "hevc": true, "vp8": true, "vp9": true, "av1": true,
	"mpeg4": true, "mpeg2video": true, "prores": true, "dnxhd": true,
	"theora": true, "wmv3": true, "vc1": true,
}

// Probe is the parsed subset of ffprobe output we need.
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

// DisplayDimensions returns width/height after rotation.
func (p Probe) DisplayDimensions() (int, int) {
	return DisplayDimensions(p.Width, p.Height, p.Rotation)
}


// ffprobe output shapes


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

type Prober struct{ Path string }

func NewProber(path string) *Prober {
	if path == "" {
		path = "ffprobe"
	}
	return &Prober{Path: path}
}

// Run probes the input (usually a presigned URL).
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

// ParseProbe turns raw ffprobe JSON into a Probe.
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

// parseRotation normalises both the displaymatrix side data and the legacy
// rotate tag to the same convention. The two disagree by sign — the matrix
// reports -90 for the same clip the tag calls 90. Negating the side data
// value is what makes them agree.
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

// parseFrameRate handles "24000/1001" style rationals.
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


// Rejection rules


// Validate checks the probe result against our rules and returns a RejectError
// for anything that shouldn't be retried.
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

	// use display dimensions — a rotated portrait is valid
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

// ffprobe reports comma-separated format lists, so we check membership
var contentTypeFormats = map[string][]string{
	"video/mp4":        {"mp4", "mov", "m4a", "isom"},
	"video/quicktime":  {"mov", "mp4"},
	"video/x-matroska": {"matroska", "webm"},
	"video/webm":       {"webm", "matroska"},
	"video/mpeg":       {"mpeg", "mpegts", "mpegvideo"},
	"video/x-msvideo":  {"avi"},
	"video/avi":        {"avi"},
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
