package transcode

import (
	"fmt"
	"path/filepath"
	"strconv"
)

// GOP alignment. These four values are the reason ABR switching works, and they
// MUST be identical on every rung:
//
//	-r <FrameRate>        pin the frame rate, so -g means a fixed wall-clock time
//	-g <GOPFrames>        keyframe every 2 seconds at 24 fps
//	-keyint_min <same>    forbid a shorter GOP
//	-sc_threshold 0       forbid EXTRA keyframes at scene changes
//
// Without -sc_threshold 0, FFmpeg inserts a keyframe wherever it detects a cut.
// Those cuts land at the same source timestamps on every rung, but the encoder's
// decision differs per rung with the bitrate, so the GOPs drift apart and a
// player switching quality mid-stream lands mid-GOP and glitches.
//
// SegmentSeconds must be an integer multiple of GOPFrames/FrameRate (6 = 3 x 2s)
// or segments do not start on keyframes and independent_segments is a lie.
const (
	FrameRate      = 24
	GOPFrames      = 48 // 2 seconds at 24 fps
	SegmentSeconds = 6  // 3 GOPs
	FFmpegThreads  = 4
)

func init() {
	gopSeconds := float64(GOPFrames) / float64(FrameRate)
	if r := float64(SegmentSeconds) / gopSeconds; r != float64(int(r)) {
		panic(fmt.Sprintf(
			"HLS segment length %ds is not a multiple of the %.2fs GOP: segments will not start on keyframes",
			SegmentSeconds, gopSeconds))
	}
}

// RenditionArgs builds the FFmpeg invocation for one rung.
//
// input is a presigned GET URL, not a local path: FFmpeg reads HTTP natively and
// range-requests only what it needs, so a 4 GB source is never downloaded.
// outDir is a local scratch directory; segments are uploaded after the encode.
func RenditionArgs(input string, r Rung, outDir string) []string {
	return []string{
		"-hide_banner",
		"-loglevel", "error",
		// FFmpeg reads stdin for interactive keypresses. In a subprocess that
		// steals the parent's input and can wedge the worker. Always set it.
		"-nostdin",
		// Machine-readable progress on stdout every 2s. Drives the progress bar
		// and, more usefully, tells you a job is alive rather than hung.
		"-progress", "pipe:1",
		"-stats_period", "2",
		"-threads", strconv.Itoa(FFmpegThreads),

		"-i", input,

		// --- video ---------------------------------------------------------
		"-c:v", "libx264",
		// Compatibility floor for older devices and smart TVs. Also what the
		// master playlist's CODECS string is derived from - see ladder.go.
		"-profile:v", "main",
		"-level", "4.0",
		"-preset", r.Preset,
		"-crf", strconv.Itoa(r.CRF),
		"-maxrate", strconv.Itoa(r.MaxRate),
		"-bufsize", strconv.Itoa(r.BufSize),

		// scale=-2 computes width from the target height, rounded to an EVEN
		// number. -1 can produce an odd width, which H.264 with yuv420p rejects.
		// force_original_aspect_ratio=decrease never distorts non-16:9 source.
		// format=yuv420p converts 10-bit / 4:2:2 sources (ProRes) to 8-bit 4:2:0;
		// omit it and the file plays in VLC but shows a black frame in Chrome.
		"-vf", fmt.Sprintf("scale=-2:%d:force_original_aspect_ratio=decrease,format=yuv420p", r.Height),

		"-g", strconv.Itoa(GOPFrames),
		"-keyint_min", strconv.Itoa(GOPFrames),
		"-sc_threshold", "0",
		"-r", strconv.Itoa(FrameRate),

		// --- audio ---------------------------------------------------------
		// Downmix to stereo at 48 kHz. 5.1 in HLS is a rabbit hole.
		"-c:a", "aac",
		"-b:a", strconv.Itoa(r.AudioBitrate),
		"-ac", "2",
		"-ar", "48000",

		// --- HLS -----------------------------------------------------------
		"-f", "hls",
		"-hls_time", strconv.Itoa(SegmentSeconds),
		// Marks the playlist complete so players enable seeking across the whole
		// timeline instead of treating it as a growing live stream.
		"-hls_playlist_type", "vod",
		"-hls_segment_type", "mpegts",
		// Declares every segment independently decodable, which is what lets a
		// player start at any segment. True only because of the GOP settings.
		"-hls_flags", "independent_segments",
		"-hls_segment_filename", filepath.Join(outDir, "seg_%05d.ts"),
		filepath.Join(outDir, "index.m3u8"),
	}
}

// PosterArgs extracts a poster frame at 10% of the duration.
//
// -ss goes BEFORE -i deliberately. Before the input it seeks by keyframe without
// decoding; after the input FFmpeg decodes from frame 0, which on a 40-minute
// source takes minutes instead of milliseconds.
//
// 10% rather than frame 0 because the first frame of almost every video is black.
func PosterArgs(input string, durationSecs float64, outPath string) []string {
	return []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-ss", strconv.FormatFloat(durationSecs*0.10, 'f', 3, 64),
		"-i", input,
		"-frames:v", "1",
		"-vf", "scale=1280:-2",
		"-q:v", "3",
		"-y",
		outPath,
	}
}

// ProbeArgs builds the ffprobe invocation.
func ProbeArgs(input string) []string {
	return []string{
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		input,
	}
}
