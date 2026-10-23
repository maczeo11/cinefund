package transcode

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These tests run real ffmpeg. The argument-string tests in ladder_test.go
// cannot catch an encoder rejecting a filter graph, which is how odd output
// widths shipped: every rung's flags looked right and 480p still failed.
// They skip when ffmpeg/ffprobe are not installed.
func TestRunRendition_EncodesEveryRungForCommonAspectRatios(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, probeErr := exec.LookPath("ffprobe")

	cases := []struct {
		name         string
		width        int
		height       int
		rungHeights  []int // which rungs to encode (kept small: this runs real encodes)
		whyItMatters string
	}{
		// 480 / 720 * 1280 = 853.33: the width that broke every 16:9 upload.
		{"16:9 720p", 1280, 720, []int{720, 480, 360, 240}, "480p is 853.33px wide"},
		// 480 / 1080 * 2560 = 1137.78 and 720 / 1080 * 2560 = 1706.67.
		{"21:9 ultrawide", 2560, 1080, []int{720, 480}, "both rungs have fractional widths"},
		// 4:3 divides evenly; guards against over-correcting.
		{"4:3", 1024, 768, []int{480, 240}, "widths are whole numbers"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src.mp4")
			gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin", "-y",
				"-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=%dx%d:rate=24", tc.width, tc.height),
				"-t", "2", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", src)
			if out, err := gen.CombinedOutput(); err != nil {
				t.Skipf("cannot generate a test clip (ffmpeg without libx264?): %v: %s", err, out)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			runner := NewRunner(ffmpeg)

			for _, rung := range LadderFor(tc.height) {
				if !containsInt(tc.rungHeights, rung.Height) {
					continue
				}
				outDir := filepath.Join(dir, rung.Name)
				if err := runner.RunRendition(ctx, src, rung, outDir, 2_000_000, nil); err != nil {
					t.Fatalf("%s rung %s (%s): %v", tc.name, rung.Name, tc.whyItMatters, err)
				}
				if probeErr != nil {
					continue
				}
				w, h := probeSize(t, ffprobe, filepath.Join(outDir, "seg_00000.ts"))
				if w%2 != 0 || h%2 != 0 {
					t.Errorf("%s rung %s: output is %dx%d, dimensions must be even", tc.name, rung.Name, w, h)
				}
				if h != rung.Height {
					t.Errorf("%s rung %s: output height %d, want %d", tc.name, rung.Name, h, rung.Height)
				}
				// The master playlist advertises Rung.Width; it must match what was encoded.
				if want := rung.Width(tc.width, tc.height); w != want {
					t.Errorf("%s rung %s: encoded width %d but the master playlist advertises %d", tc.name, rung.Name, w, want)
				}
			}
		})
	}
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func probeSize(t *testing.T, ffprobe, file string) (int, int) {
	t.Helper()
	out, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height", "-of", "csv=p=0", file).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", file, err)
	}
	// MPEG-TS segments report each stream more than once, separated by blank
	// lines; the first line is the one we want.
	first, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	parts := strings.Split(strings.TrimSpace(first), ",")
	if len(parts) != 2 {
		t.Fatalf("unexpected ffprobe output %q", out)
	}
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[1])
	return w, h
}
