package transcode

import (
	"math"
	"strings"
	"testing"
)

func TestTrackProgress_ParsesBlocks(t *testing.T) {
	in := strings.Join([]string{
		"frame=240", "fps=52.30", "out_time_us=10000000", "speed=2.18x", "progress=continue",
		"frame=480", "fps=51.10", "out_time_us=20000000", "speed=2.10x", "progress=continue",
		"frame=720", "out_time_us=30000000", "speed=2.05x", "progress=end",
		"",
	}, "\n")

	var got []Progress
	TrackProgress(strings.NewReader(in), 30_000_000, func(p Progress) { got = append(got, p) })

	if len(got) != 3 {
		t.Fatalf("got %d updates, want 3", len(got))
	}
	if math.Abs(got[0].Fraction-1.0/3.0) > 0.001 {
		t.Errorf("first fraction = %v, want ~0.333", got[0].Fraction)
	}
	if got[0].Speed != 2.18 {
		t.Errorf("first speed = %v, want 2.18", got[0].Speed)
	}
	if got[2].Fraction != 1.0 {
		t.Errorf("final fraction = %v, want 1.0", got[2].Fraction)
	}
	if got[1].Frame != 480 {
		t.Errorf("second frame = %d, want 480", got[1].Frame)
	}
}

// out_time_us reports N/A for the first block or two. Letting that fall through
// to zero resets the bar mid-encode, which looks like the job restarted.
func TestTrackProgress_SurvivesNotAvailable(t *testing.T) {
	in := strings.Join([]string{
		"out_time_us=N/A", "speed=0x", "progress=continue",
		"out_time_us=15000000", "speed=1.5x", "progress=continue",
		"out_time_us=N/A", "progress=continue",
		"",
	}, "\n")

	var got []Progress
	TrackProgress(strings.NewReader(in), 30_000_000, func(p Progress) { got = append(got, p) })

	if len(got) != 3 {
		t.Fatalf("got %d updates, want 3", len(got))
	}
	if got[0].Fraction != 0 {
		t.Errorf("first fraction = %v, want 0", got[0].Fraction)
	}
	if math.Abs(got[1].Fraction-0.5) > 0.001 {
		t.Errorf("second fraction = %v, want 0.5", got[1].Fraction)
	}
	// The N/A after a real value must not reset progress.
	if got[2].Fraction != got[1].Fraction {
		t.Errorf("progress went backwards on N/A: %v -> %v", got[1].Fraction, got[2].Fraction)
	}
}

func TestTrackProgress_NeverExceedsOne(t *testing.T) {
	in := "out_time_us=99000000\nprogress=continue\n"
	var got Progress
	TrackProgress(strings.NewReader(in), 30_000_000, func(p Progress) { got = p })
	if got.Fraction != 1.0 {
		t.Errorf("fraction = %v, want clamped to 1.0", got.Fraction)
	}
}

// The 1080p rung costs far more than 360p, so a plain mean makes the bar leap
// when cheap rungs finish and then stall for minutes on the expensive one.
func TestOverallProgress_IsWeighted(t *testing.T) {
	ladder := LadderFor(1080)
	tasks := NewTasks(ladder)

	// Finish everything except 1080p.
	for i := range tasks {
		if tasks[i].Rung != "1080p" {
			tasks[i].Progress = 1
			tasks[i].Done = true
		}
	}

	got := OverallProgress(tasks)
	plainMean := float64(len(tasks)-1) / float64(len(tasks)) // 0.8

	if got >= plainMean {
		t.Errorf("weighted progress %v should be below the plain mean %v", got, plainMean)
	}
	if got <= 0 || got >= 1 {
		t.Errorf("progress %v out of range", got)
	}
}

func TestOverallProgress_Empty(t *testing.T) {
	if got := OverallProgress(nil); got != 0 {
		t.Errorf("empty progress = %v, want 0", got)
	}
}
