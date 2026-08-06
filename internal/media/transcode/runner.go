package transcode

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Runner executes FFmpeg. Split from Worker so the encode of a single rendition
// is testable on its own against a real sample file.
type Runner struct {
	FFmpegPath string
	// KillGrace is how long FFmpeg gets after SIGTERM before SIGKILL.
	KillGrace time.Duration
}

func NewRunner(ffmpegPath string) *Runner {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return &Runner{FFmpegPath: ffmpegPath, KillGrace: 10 * time.Second}
}

// command builds an exec.Cmd wired for graceful cancellation.
//
// exec.CommandContext on its own sends SIGKILL when the context is cancelled,
// which leaves half-written segment files behind. SIGTERM lets FFmpeg finish the
// segment it is on and close its files cleanly. WaitDelay is the escalation:
// without it a wedged FFmpeg that ignores SIGTERM blocks shutdown forever.
func (r *Runner) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, r.FFmpegPath, args...)
	cmd.Cancel = func() error { return signalTerminate(cmd) }
	cmd.WaitDelay = r.KillGrace
	return cmd
}

// RunRendition encodes one rung into outDir, reporting progress as it goes.
func (r *Runner) RunRendition(
	ctx context.Context,
	input string,
	rung Rung,
	outDir string,
	totalMicros int64,
	onProgress func(Progress),
) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create rendition dir: %w", err)
	}

	cmd := r.command(ctx, RenditionArgs(input, rung, outDir)...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		TrackProgress(stdout, totalMicros, func(p Progress) {
			if onProgress != nil {
				onProgress(p)
			}
		})
	}()
	wg.Wait() // stdout closes when FFmpeg exits

	if err := cmd.Wait(); err != nil {
		// A cancelled context is an abort, not an encode failure. Distinguishing
		// them matters: an abort must not burn a retry attempt.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("rendition %s aborted: %w", rung.Name, ctxErr)
		}
		return fmt.Errorf("ffmpeg %s: %w: %s", rung.Name, err, tail(stderr.String()))
	}

	// FFmpeg exits 0 having written nothing if the input had no decodable
	// frames. Assert the playlist exists rather than trusting the exit code.
	index := filepath.Join(outDir, "index.m3u8")
	if st, err := os.Stat(index); err != nil || st.Size() == 0 {
		return fmt.Errorf("rendition %s produced no playlist: %s", rung.Name, tail(stderr.String()))
	}
	return nil
}

// ExtractPoster writes a poster frame. Failure is not fatal to a job: a film
// without a thumbnail is still watchable, so callers log and continue.
func (r *Runner) ExtractPoster(ctx context.Context, input string, durationSecs float64, outPath string) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	cmd := r.command(ctx, PosterArgs(input, durationSecs, outPath)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("poster: %w: %s", err, tail(stderr.String()))
	}
	return nil
}

// tail keeps the last few lines of FFmpeg stderr. The useful error is always at
// the end, and the whole thing can be megabytes.
func tail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(no stderr)"
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
	}
	return strings.Join(lines, " | ")
}
