package transcode

import (
	"strings"
	"testing"
)

// a 480p source yields exactly the rungs at or below 480p, none upscaled.
func TestLadderFor_NeverUpscales(t *testing.T) {
	cases := []struct {
		srcHeight int
		want      []string
	}{
		{1080, []string{"1080p", "720p", "480p", "360p", "240p"}},
		{720, []string{"720p", "480p", "360p", "240p"}},
		{480, []string{"480p", "360p", "240p"}},
		{360, []string{"360p", "240p"}},
		{240, []string{"240p"}},
		// Below the lowest rung: something still has to play.
		{144, []string{"240p"}},
	}

	for _, c := range cases {
		got := LadderFor(c.srcHeight)
		if len(got) != len(c.want) {
			t.Fatalf("height %d: got %d rungs, want %d (%v)", c.srcHeight, len(got), len(c.want), names(got))
		}
		for i, r := range got {
			if r.Name != c.want[i] {
				t.Errorf("height %d: rung %d = %s, want %s", c.srcHeight, i, r.Name, c.want[i])
			}
			if c.srcHeight >= 240 && r.Height > c.srcHeight {
				t.Errorf("height %d: rung %s upscales", c.srcHeight, r.Name)
			}
		}
	}
}

// the ladder decision must use DISPLAY dimensions. A 90-rotated 1920x1080 clip
// displays as 1080x1920 portrait, so it must get a ladder built from height
// 1920, not 1080.
func TestDisplayDimensions_Rotation(t *testing.T) {
	cases := []struct {
		w, h, rot int
		wantW     int
		wantH     int
	}{
		{1920, 1080, 0, 1920, 1080},
		{1920, 1080, 180, 1920, 1080},
		{1920, 1080, 90, 1080, 1920},
		{1920, 1080, 270, 1080, 1920},
	}
	for _, c := range cases {
		gw, gh := DisplayDimensions(c.w, c.h, c.rot)
		if gw != c.wantW || gh != c.wantH {
			t.Errorf("rotation %d: got %dx%d, want %dx%d", c.rot, gw, gh, c.wantW, c.wantH)
		}
	}

	// The consequence: a rotated clip must not be treated as landscape.
	_, portraitH := DisplayDimensions(1920, 1080, 90)
	if got := LadderFor(portraitH); got[0].Name != "1080p" {
		t.Errorf("rotated source got top rung %s, want 1080p", got[0].Name)
	}
}

// The GOP invariant. If this fails, ABR switching glitches - and it shows up
// as playback bugs that are hard to trace back to the encoder settings.
func TestGOPAlignmentInvariant(t *testing.T) {
	gopSeconds := float64(GOPFrames) / float64(FrameRate)
	if gopSeconds != 2.0 {
		t.Errorf("GOP is %.2fs, expected 2s", gopSeconds)
	}
	if r := float64(SegmentSeconds) / gopSeconds; r != float64(int(r)) {
		t.Fatalf("segment length %ds is not a whole number of %.2fs GOPs", SegmentSeconds, gopSeconds)
	}
}

// Every rung must carry the identical GOP settings, or the rungs desynchronise.
func TestRenditionArgs_GOPIdenticalAcrossRungs(t *testing.T) {
	want := map[string]string{
		"-g":            "48",
		"-keyint_min":   "48",
		"-sc_threshold": "0",
		"-r":            "24",
	}
	for _, rung := range LadderFor(1080) {
		args := RenditionArgs("http://example/in.mp4", rung, "/tmp/out")
		for flag, val := range want {
			if got := argValue(args, flag); got != val {
				t.Errorf("rung %s: %s = %q, want %q", rung.Name, flag, got, val)
			}
		}
	}
}

// The flags whose absence produces bugs that look like something else.
func TestRenditionArgs_CriticalFlags(t *testing.T) {
	args := RenditionArgs("http://example/in.mp4", fullLadder[0], "/tmp/out")
	joined := strings.Join(args, " ")

	// yuv420p: without it, 10-bit ProRes plays in VLC and shows black in Chrome.
	if !strings.Contains(joined, "format=yuv420p") {
		t.Error("missing format=yuv420p in the filter chain")
	}
	// scale=-2 keeps width even; -1 can produce an odd width that H.264 rejects.
	if !strings.Contains(joined, "scale=-2:") {
		t.Error("expected scale=-2 for even width")
	}
	if !strings.Contains(joined, "force_original_aspect_ratio=decrease") {
		t.Error("missing force_original_aspect_ratio=decrease")
	}
	// -nostdin: without it FFmpeg steals the parent's stdin and can wedge.
	if !hasFlag(args, "-nostdin") {
		t.Error("missing -nostdin")
	}
	if argValue(args, "-hls_playlist_type") != "vod" {
		t.Error("expected -hls_playlist_type vod so players can seek")
	}
	if argValue(args, "-hls_flags") != "independent_segments" {
		t.Error("expected -hls_flags independent_segments")
	}
}

// -ss must come BEFORE -i, or FFmpeg decodes from frame 0 and a 40-minute source
// takes minutes instead of milliseconds.
func TestPosterArgs_SeekBeforeInput(t *testing.T) {
	args := PosterArgs("http://example/in.mp4", 100, "/tmp/poster.webp")
	ssIdx, inIdx := indexOf(args, "-ss"), indexOf(args, "-i")
	if ssIdx < 0 || inIdx < 0 {
		t.Fatalf("expected both -ss and -i in %v", args)
	}
	if ssIdx > inIdx {
		t.Errorf("-ss at %d is after -i at %d: this decodes from the start", ssIdx, inIdx)
	}
	// 10% of duration, not frame 0 (which is almost always black).
	if got := argValue(args, "-ss"); got != "10.000" {
		t.Errorf("-ss = %q, want 10.000 (10%% of 100s)", got)
	}
}

func names(rs []Rung) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Name
	}
	return out
}

func indexOf(args []string, flag string) int {
	for i, a := range args {
		if a == flag {
			return i
		}
	}
	return -1
}

func hasFlag(args []string, flag string) bool { return indexOf(args, flag) >= 0 }

func argValue(args []string, flag string) string {
	i := indexOf(args, flag)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}
