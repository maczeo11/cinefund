package transcode

import (
	"fmt"
	"strings"
	"testing"
)

type fakePresigner struct{ err error }

func (f fakePresigner) PresignedGet(key string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "https://store.example/" + key + "?sig=abc", nil
}

func TestBuildMasterPlaylist_HighestBandwidthFirst(t *testing.T) {
	out := string(BuildMasterPlaylist(LadderFor(1080), 1920, 1080))
	lines := strings.Split(strings.TrimSpace(out), "\n")

	if lines[0] != "#EXTM3U" {
		t.Fatalf("first line = %q", lines[0])
	}

	var order []string
	for _, l := range lines {
		if strings.HasSuffix(l, "/index.m3u8") {
			order = append(order, strings.TrimSuffix(l, "/index.m3u8"))
		}
	}
	want := []string{"1080p", "720p", "480p", "360p", "240p"}
	if len(order) != len(want) {
		t.Fatalf("got %d variants, want %d: %v", len(order), len(want), order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("variant %d = %s, want %s", i, order[i], want[i])
		}
	}
}

// The CODECS string must match what the encoder actually produces. Advertising
// High profile for a Main-encoded stream is the "works in Chrome, black screen
// on iPhone" bug.
func TestBuildMasterPlaylist_CodecsMatchEncoderSettings(t *testing.T) {
	out := string(BuildMasterPlaylist(LadderFor(1080), 1920, 1080))

	// args.go pins -profile:v main -level 4.0 for every rung.
	const wantCodec = "avc1.4d0028"
	if codecString() != wantCodec {
		t.Fatalf("codecString() = %s, want %s", codecString(), wantCodec)
	}
	if strings.Count(out, wantCodec) != 5 {
		t.Errorf("expected every variant to advertise %s:\n%s", wantCodec, out)
	}
	// The High-profile string from the doc's example must not appear.
	if strings.Contains(out, "avc1.640028") {
		t.Error("master advertises High profile but the encoder produces Main")
	}
}

// BANDWIDTH is the peak; understating it makes players choose a rung they
// cannot sustain.
func TestBuildMasterPlaylist_BandwidthIsPeakNotAverage(t *testing.T) {
	r := fullLadder[0] // 1080p
	out := string(BuildMasterPlaylist([]Rung{r}, 1920, 1080))

	wantPeak := fmt.Sprintf("BANDWIDTH=%d", r.MaxRate+r.AudioBitrate)
	wantAvg := fmt.Sprintf("AVERAGE-BANDWIDTH=%d", r.VideoBitrate+r.AudioBitrate)
	if !strings.Contains(out, wantPeak) {
		t.Errorf("missing %s in:\n%s", wantPeak, out)
	}
	if !strings.Contains(out, wantAvg) {
		t.Errorf("missing %s in:\n%s", wantAvg, out)
	}
	if r.MaxRate <= r.VideoBitrate {
		t.Error("maxrate should exceed the target bitrate")
	}
}

func TestRungWidth_IsEven(t *testing.T) {
	// 1440x1080 is 4:3. 720p from it is 960 wide - even. Awkward ratios must
	// still round to even or H.264 with yuv420p rejects the dimensions.
	for _, src := range [][2]int{{1920, 1080}, {1440, 1080}, {1080, 1920}, {1234, 987}} {
		for _, r := range LadderFor(src[1]) {
			if w := r.Width(src[0], src[1]); w%2 != 0 {
				t.Errorf("source %dx%d rung %s: width %d is odd", src[0], src[1], r.Name, w)
			}
		}
	}
}

func TestRewriteVariantPlaylist_SignsSegments(t *testing.T) {
	in := []byte(strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-VERSION:3",
		"#EXT-X-TARGETDURATION:6",
		"#EXTINF:6.000,",
		"seg_00000.ts",
		"#EXTINF:6.000,",
		"seg_00001.ts",
		"#EXT-X-ENDLIST",
		"",
	}, "\n"))

	out, err := RewriteVariantPlaylist(in, "asset-1", 1, "720p", fakePresigner{})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	s := string(out)

	if strings.Contains(s, "\nseg_00000.ts\n") {
		t.Error("segment left unsigned")
	}
	if !strings.Contains(s, "renditions/asset-1/v1/720p/seg_00000.ts?sig=abc") {
		t.Errorf("expected a signed segment URL, got:\n%s", s)
	}
	// Directives must survive untouched.
	for _, tag := range []string{"#EXTM3U", "#EXT-X-TARGETDURATION:6", "#EXT-X-ENDLIST"} {
		if !strings.Contains(s, tag) {
			t.Errorf("lost directive %s", tag)
		}
	}
}

// M12: a crafted path in a playlist must not become a signed URL to an
// arbitrary object. The playlist is a file we generated, but treating it as
// untrusted input costs two lines.
func TestRewriteVariantPlaylist_RejectsTraversal(t *testing.T) {
	for _, bad := range []string{
		"../../../etc/passwd",
		"../seg_00000.ts",
		"other/seg_00000.ts",
		"..\\windows\\system32",
	} {
		in := []byte("#EXTM3U\n#EXTINF:6.000,\n" + bad + "\n")
		if _, err := RewriteVariantPlaylist(in, "asset-1", 1, "720p", fakePresigner{}); err == nil {
			t.Errorf("accepted traversal line %q", bad)
		}
	}
}

func TestRenditionPrefix_IsDeterministic(t *testing.T) {
	a := RenditionPrefix("asset-1", 1, "720p")
	b := RenditionPrefix("asset-1", 1, "720p")
	if a != b {
		t.Fatal("prefix is not deterministic")
	}
	if a != "renditions/asset-1/v1/720p" {
		t.Errorf("prefix = %q", a)
	}
	// Bumping the pipeline version produces a separate tree, so the old one can
	// be dropped by lifecycle rule once the new one is live.
	if RenditionPrefix("asset-1", 2, "720p") == a {
		t.Error("pipeline version does not affect the prefix")
	}
}
