package transcode

import (
	"errors"
	"strings"
	"testing"
)

const audioOnlyJSON = `{
  "streams": [{"codec_type":"audio","codec_name":"aac"}],
  "format": {"duration":"180.5","size":"2400000","format_name":"mov,mp4,m4a,3gp,3g2,mj2"}
}`

const video1080JSON = `{
  "streams": [
    {"codec_type":"video","codec_name":"h264","width":1920,"height":1080,
     "pix_fmt":"yuv420p","r_frame_rate":"24000/1001"},
    {"codec_type":"audio","codec_name":"aac"}
  ],
  "format": {"duration":"600.0","size":"400000000","format_name":"mov,mp4,m4a,3gp,3g2,mj2"}
}`

const rotatedJSON = `{
  "streams": [
    {"codec_type":"video","codec_name":"h264","width":1920,"height":1080,
     "pix_fmt":"yuv420p","r_frame_rate":"30/1",
     "side_data_list":[{"side_data_type":"Display Matrix","rotation":-90}]}
  ],
  "format": {"duration":"30.0","size":"20000000","format_name":"mov,mp4,m4a,3gp,3g2,mj2"}
}`

const tinyJSON = `{
  "streams": [{"codec_type":"video","codec_name":"h264","width":320,"height":240,"pix_fmt":"yuv420p"}],
  "format": {"duration":"12.0","size":"100000","format_name":"mov,mp4"}
}`

func mustParse(t *testing.T, raw string) *Probe {
	t.Helper()
	p, err := ParseProbe([]byte(raw))
	if err != nil {
		t.Fatalf("ParseProbe: %v", err)
	}
	return p
}

func assertRejected(t *testing.T, err error, want RejectReason) {
	t.Helper()
	var re *RejectError
	if !errors.As(err, &re) {
		t.Fatalf("expected a RejectError, got %v", err)
	}
	if re.Reason != want {
		t.Errorf("reason = %q, want %q", re.Reason, want)
	}
}

// an audio-only file is rejected, and no FFmpeg is ever invoked because the
// rejection happens on the probe result.
func TestValidate_AudioOnlyRejected(t *testing.T) {
	err := mustParse(t, audioOnlyJSON).Validate(PurposeFilm, "video/mp4")
	assertRejected(t, err, ReasonNoVideoStream)
}

func TestValidate_HappyPath(t *testing.T) {
	if err := mustParse(t, video1080JSON).Validate(PurposeFilm, "video/mp4"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidate_DurationLimits(t *testing.T) {
	long := strings.Replace(video1080JSON, `"duration":"600.0"`, `"duration":"4000.0"`, 1)
	assertRejected(t, mustParse(t, long).Validate(PurposeFilm, "video/mp4"), ReasonTooLong)

	// The same file is fine as a FILM at 600s but too long as a PITCH.
	assertRejected(t, mustParse(t, video1080JSON).Validate(PurposePitch, "video/mp4"), ReasonPitchTooLong)

	zero := strings.Replace(video1080JSON, `"duration":"600.0"`, `"duration":"0"`, 1)
	assertRejected(t, mustParse(t, zero).Validate(PurposeFilm, "video/mp4"), ReasonBadDuration)
}

func TestValidate_ResolutionFloor(t *testing.T) {
	assertRejected(t, mustParse(t, tinyJSON).Validate(PurposeFilm, "video/mp4"), ReasonResolutionTooLow)
}

// A portrait clip is 1080 wide and 1920 tall once rotation is applied. It must
// NOT be rejected for being narrower than the 640 minimum on the stored width.
func TestValidate_RotatedPortraitAccepted(t *testing.T) {
	p := mustParse(t, rotatedJSON)
	if p.Rotation != 90 {
		t.Fatalf("rotation = %d, want 90", p.Rotation)
	}
	w, h := p.DisplayDimensions()
	if w != 1080 || h != 1920 {
		t.Fatalf("display dims = %dx%d, want 1080x1920", w, h)
	}
	if err := p.Validate(PurposeFilm, "video/mp4"); err != nil {
		t.Errorf("rotated portrait rejected: %v", err)
	}
}

func TestValidate_UnsupportedCodec(t *testing.T) {
	weird := strings.Replace(video1080JSON, `"codec_name":"h264"`, `"codec_name":"cinepak"`, 1)
	assertRejected(t, mustParse(t, weird).Validate(PurposeFilm, "video/mp4"), ReasonUnsupportedCodec)
}

// A client that presigned for video/mp4 and uploaded a Matroska file is either
// broken or probing. Either way it does not get transcoded.
func TestValidate_ContentTypeMismatch(t *testing.T) {
	mkv := strings.Replace(video1080JSON,
		`"format_name":"mov,mp4,m4a,3gp,3g2,mj2"`, `"format_name":"matroska,webm"`, 1)
	assertRejected(t, mustParse(t, mkv).Validate(PurposeFilm, "video/mp4"), ReasonTypeMismatch)

	// The same file declared honestly is fine.
	if err := mustParse(t, mkv).Validate(PurposeFilm, "video/webm"); err != nil {
		t.Errorf("honest declaration rejected: %v", err)
	}
	// An unrecognised declared type is not evidence of an attack.
	if err := mustParse(t, video1080JSON).Validate(PurposeFilm, "application/octet-stream"); err != nil {
		t.Errorf("unknown content type should not reject: %v", err)
	}
}

func TestParseFrameRate(t *testing.T) {
	cases := map[string]float64{
		"24000/1001": 24000.0 / 1001.0,
		"30/1":       30,
		"25":         25,
		"0/0":        0,
		"garbage":    0,
	}
	for in, want := range cases {
		if got := parseFrameRate(in); got != want {
			t.Errorf("parseFrameRate(%q) = %v, want %v", in, got, want)
		}
	}
}
