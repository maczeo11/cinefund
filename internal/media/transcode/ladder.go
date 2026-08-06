package transcode

import "fmt"

// Rung is one step of the ABR ladder.
//
// MaxRate and BufSize are not decoration: CRF alone gives consistent visual
// quality but lets the bitrate spike far above the rung's advertised bandwidth
// on a busy scene, which stalls a client that selected that rung on exactly the
// assumption the playlist made. Constrained-quality encoding is CRF plus a cap.
type Rung struct {
	Name         string
	Height       int
	VideoBitrate int // bits/sec - the target, and AVERAGE-BANDWIDTH in the master
	MaxRate      int // = VideoBitrate * 1.07 - the cap, and the basis for BANDWIDTH
	BufSize      int // = VideoBitrate * 1.5
	AudioBitrate int
	Preset       string
	CRF          int
}

// PeakBandwidth is what goes in EXT-X-STREAM-INF:BANDWIDTH. It must be the peak
// including audio: understating it makes players pick a rung they cannot
// sustain, which shows up as rebuffering that looks like a network problem.
func (r Rung) PeakBandwidth() int { return r.MaxRate + r.AudioBitrate }

// AverageBandwidth is the target, including audio.
func (r Rung) AverageBandwidth() int { return r.VideoBitrate + r.AudioBitrate }

// Width returns the even width for a given source aspect ratio. H.264 with
// yuv420p requires even dimensions in both axes; FFmpeg's scale=-2 does this at
// encode time and this mirrors it for the RESOLUTION field in the master.
func (r Rung) Width(srcW, srcH int) int {
	if srcH == 0 {
		return 0
	}
	w := int(float64(r.Height) * float64(srcW) / float64(srcH))
	return w &^ 1 // round down to even
}

var fullLadder = []Rung{
	{"1080p", 1080, 5_000_000, 5_350_000, 7_500_000, 128_000, "medium", 21},
	{"720p", 720, 2_800_000, 2_996_000, 4_200_000, 128_000, "medium", 22},
	{"480p", 480, 1_400_000, 1_498_000, 2_100_000, 96_000, "fast", 23},
	{"360p", 360, 800_000, 856_000, 1_200_000, 64_000, "fast", 24},
	{"240p", 240, 400_000, 428_000, 600_000, 48_000, "veryfast", 26},
}

// LadderFor returns the rungs at or below the source height, highest first.
//
// Never upscale. Encoding a 480p source at 1080p produces a bigger file that
// looks identical-to-worse and burns roughly 4x the CPU - the single most common
// mistake in a homegrown transcoder. A source shorter than the lowest rung still
// gets that lowest rung, because something has to play.
func LadderFor(sourceHeight int) []Rung {
	out := make([]Rung, 0, len(fullLadder))
	for _, r := range fullLadder {
		if r.Height <= sourceHeight {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		out = append(out, fullLadder[len(fullLadder)-1])
	}
	return out
}

// DisplayDimensions applies the rotation matrix.
//
// Phone footage stores 1920x1080 with rotation=90 and displays as 1080x1920.
// FFmpeg auto-rotates on decode so the OUTPUT is usually upright anyway, but the
// LADDER decision reads the stored numbers - so without this swap a portrait
// clip gets a landscape ladder and every rung is wrong. Tests M3 and M4.
func DisplayDimensions(width, height, rotation int) (int, int) {
	if rotation == 90 || rotation == 270 {
		return height, width
	}
	return width, height
}

// codecString is the RFC 6381 identifier for the encode settings in args.go.
//
// Every rung is encoded with `-profile:v main -level 4.0`, so every rung is
// main@4.0 -> avc1.4d4028. Deriving it here rather than hardcoding a table means
// it cannot drift from the encoder settings.
//
// NOTE: docs/09 §7 shows an example master listing four DIFFERENT codec strings
// (avc1.640028 = High@4.0 for 1080p, avc1.42c01e = Baseline@3.0 for 360p). That
// example contradicts the FFmpeg command in §4 of the same document, which pins
// main@4.0 for every rung. Advertising High profile for a stream encoded as Main
// is exactly the "works in Chrome, black screen on iPhone" bug §7 warns about,
// so the encoder settings win.
const (
	profileMainHex = 0x4d // Main
	levelHex       = 0x28 // 4.0
	constraintHex  = 0x00
)

func codecString() string {
	return fmt.Sprintf("avc1.%02x%02x%02x", profileMainHex, constraintHex, levelHex)
}

// audioCodecString is AAC-LC.
const audioCodecString = "mp4a.40.2"
