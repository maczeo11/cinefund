package transcode

import (
	"bufio"
	"io"
	"math"
	"strconv"
	"strings"
)

// Progress is one sample from FFmpeg's -progress stream.
type Progress struct {
	Fraction float64 // 0..1
	Speed    float64 // encode speed multiple, e.g. 2.18 means 2.18x realtime
	Frame    int64
}

// TrackProgress parses `-progress pipe:1` output and calls onUpdate once per
// block. It returns when the reader is exhausted, so run it in a goroutine.
//
// FFmpeg emits key=value lines terminated by `progress=continue` (or `end`):
//
//	frame=1247
//	fps=52.30
//	out_time_us=51958333
//	speed=2.18x
//	progress=continue
func TrackProgress(stdout io.Reader, totalMicros int64, onUpdate func(Progress)) {
	sc := bufio.NewScanner(stdout)
	var cur Progress

	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		switch k {
		case "frame":
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				cur.Frame = n
			}
		case "out_time_us", "out_time_ms":
			// out_time_us reports "N/A" for the first block or two. Guarding the
			// parse matters: letting it fall through to zero resets the progress
			// bar to 0% mid-encode, which looks like the job restarted.
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 || totalMicros <= 0 {
				continue
			}
			if k == "out_time_ms" {
				n *= 1000 // FFmpeg's out_time_ms is actually microseconds on some
				// builds; treat the _us key as authoritative when both appear.
			}
			cur.Fraction = math.Min(float64(n)/float64(totalMicros), 1.0)
		case "speed":
			if f, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(v), "x"), 64); err == nil {
				cur.Speed = f
			}
		case "progress":
			if v == "end" {
				cur.Fraction = 1.0
			}
			onUpdate(cur)
		}
	}
}

// Task is one rung's progress within a job.
type Task struct {
	Rung     string  `json:"rung"`
	Progress float64 `json:"progress"`
	Speed    float64 `json:"speed,omitempty"`
	Done     bool    `json:"done"`

	weight float64 // encode cost proxy; not serialised
}

// OverallProgress is a duration-weighted average, not a plain mean.
//
// The 1080p rung takes several times longer than 360p, so averaging them
// equally makes the bar leap forward when the cheap rungs finish and then appear
// to stall for minutes on the expensive one. Weighting by video bitrate is a
// decent proxy for encode cost and makes the bar move at a roughly constant rate.
func OverallProgress(tasks []Task) float64 {
	var num, den float64
	for _, t := range tasks {
		w := t.weight
		if w <= 0 {
			w = 1
		}
		num += t.Progress * w
		den += w
	}
	if den == 0 {
		return 0
	}
	return num / den
}

// NewTasks builds the task list for a ladder, pre-weighted by encode cost.
func NewTasks(ladder []Rung) []Task {
	out := make([]Task, len(ladder))
	for i, r := range ladder {
		out[i] = Task{Rung: r.Name, weight: float64(r.VideoBitrate)}
	}
	return out
}
