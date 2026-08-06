package transcode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Job is what a worker claims. Declared here rather than imported from the media
// package because the consumer declares the interface it needs.
type Job struct {
	ID              uuid.UUID
	AssetID         uuid.UUID
	PipelineVersion int
	Attempt         int

	// Asset fields the worker needs, denormalised by the claim query.
	StorageKey  string
	ContentType string
	Purpose     Purpose
}

// JobStore is the persistence the worker needs. Every method is scoped by
// workerID where a stale worker could otherwise do damage.
type JobStore interface {
	// Claim atomically takes one QUEUED job, or one whose lease has expired.
	// Returns (nil, nil) when there is nothing to do.
	Claim(ctx context.Context, workerID string, leaseTTL time.Duration) (*Job, error)

	// Heartbeat extends the lease and records progress. It returns false when
	// the update matched no rows, which means another worker reclaimed the job.
	Heartbeat(ctx context.Context, jobID uuid.UUID, workerID string,
		leaseTTL time.Duration, progress, speed float64, tasks []byte) (bool, error)

	// Succeed marks the job done and the asset READY.
	Succeed(ctx context.Context, jobID, assetID uuid.UUID, workerID string,
		masterKey, posterKey string, renditions []byte, probe []byte,
		durationSecs float64, width, height, rotation int) error

	// Fail marks a retryable failure. The job goes back to QUEUED unless
	// attempts are exhausted.
	Fail(ctx context.Context, jobID uuid.UUID, workerID string, reason string, retryable bool) error

	// Reject marks the asset permanently REJECTED. Never retried.
	Reject(ctx context.Context, jobID, assetID uuid.UUID, workerID string,
		reason RejectReason, detail string, probe []byte) error
}

// ObjectStore is the subset of storage the worker uses. io.Reader rather than a
// path so a test can supply an in-memory store with no filesystem.
type ObjectStore interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	PresignedGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// Config tunes the worker.
type Config struct {
	WorkerID      string
	Concurrency   int           // simultaneous FFmpeg processes
	LeaseTTL      time.Duration // how long a claim is valid
	HeartbeatEach time.Duration // must be well under LeaseTTL
	JobTimeout    time.Duration
	MaxAttempts   int
	WorkDir       string
	PollInterval  time.Duration
}

func (c *Config) applyDefaults() {
	if c.WorkerID == "" {
		c.WorkerID = uuid.NewString()
	}
	if c.Concurrency <= 0 {
		// FFmpeg is CPU-bound and already multi-threaded. Running NumCPU
		// concurrent encodes gives each ~1 core, so all of them finish slowly
		// with NumCPU times the peak memory. Four threads per encode and
		// NumCPU/4 concurrent encodes keeps each one fast.
		c.Concurrency = max(1, runtime.NumCPU()/4)
	}
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = 60 * time.Second
	}
	if c.HeartbeatEach <= 0 {
		c.HeartbeatEach = c.LeaseTTL / 3
	}
	if c.JobTimeout <= 0 {
		c.JobTimeout = 2 * time.Hour
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 3
	}
	if c.WorkDir == "" {
		c.WorkDir = filepath.Join(os.TempDir(), "cinefund")
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 2 * time.Second
	}
}

// ErrJobStolen means the lease was reclaimed by another worker mid-job.
var ErrJobStolen = errors.New("job lease reclaimed by another worker")

// Worker claims transcode jobs and runs them.
type Worker struct {
	cfg    Config
	jobs   JobStore
	store  ObjectStore
	runner *Runner
	prober *Prober
	log    *slog.Logger

	sem chan struct{} // bounds concurrent FFmpeg processes across all jobs
}

func NewWorker(cfg Config, jobs JobStore, store ObjectStore, runner *Runner, prober *Prober, log *slog.Logger) *Worker {
	cfg.applyDefaults()
	return &Worker{
		cfg:    cfg,
		jobs:   jobs,
		store:  store,
		runner: runner,
		prober: prober,
		log:    log.With("worker_id", cfg.WorkerID),
		sem:    make(chan struct{}, cfg.Concurrency),
	}
}

// Run polls for jobs until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("transcoder started", "concurrency", w.cfg.Concurrency, "work_dir", w.cfg.WorkDir)
	t := time.NewTicker(w.cfg.PollInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info("transcoder stopping")
			return nil
		case <-t.C:
			job, err := w.jobs.Claim(ctx, w.cfg.WorkerID, w.cfg.LeaseTTL)
			if err != nil {
				w.log.Error("claim failed", "error", err)
				continue
			}
			if job == nil {
				continue
			}
			w.process(ctx, job)
		}
	}
}

// process runs one job to a terminal state. It never returns an error: every
// outcome is recorded on the job.
func (w *Worker) process(parent context.Context, job *Job) {
	log := w.log.With("job_id", job.ID, "asset_id", job.AssetID, "attempt", job.Attempt)
	log.Info("job claimed")

	// jobCtx is cancelled by the timeout, by shutdown, OR by the heartbeat
	// discovering the lease was stolen. Everything downstream - including the
	// FFmpeg subprocess - hangs off it, so a steal stops the encode rather than
	// letting two workers write the same rendition keys concurrently. That is
	// the one genuinely corrupting failure mode in this pipeline.
	jobCtx, cancel := context.WithTimeout(parent, w.cfg.JobTimeout)
	defer cancel()

	state := &jobState{tasks: nil}
	stolen := make(chan struct{})
	var stolenOnce sync.Once

	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		w.heartbeat(jobCtx, job, state, func() {
			stolenOnce.Do(func() { close(stolen) })
			cancel()
		})
	}()

	err := w.runJob(jobCtx, job, state)
	cancel()
	<-hbDone

	select {
	case <-stolen:
		// Another worker owns this job now. Write nothing: any status update
		// here would stomp on the new owner's progress.
		log.Warn("job was reclaimed by another worker; abandoning without writing")
		return
	default:
	}

	switch {
	case err == nil:
		log.Info("job succeeded")

	case isReject(err):
		var re *RejectError
		errors.As(err, &re)
		log.Info("asset rejected", "reason", re.Reason, "detail", re.Detail)
		if ferr := w.jobs.Reject(context.WithoutCancel(parent), job.ID, job.AssetID,
			w.cfg.WorkerID, re.Reason, re.Detail, state.probeRaw); ferr != nil {
			log.Error("failed to record rejection", "error", ferr)
		}

	default:
		retryable := job.Attempt < w.cfg.MaxAttempts && parent.Err() == nil
		log.Error("job failed", "error", err, "retryable", retryable)
		if ferr := w.jobs.Fail(context.WithoutCancel(parent), job.ID,
			w.cfg.WorkerID, err.Error(), retryable); ferr != nil {
			log.Error("failed to record failure", "error", ferr)
		}
	}
}

type jobState struct {
	mu       sync.Mutex
	tasks    []Task
	speed    float64
	probeRaw []byte
}

func (s *jobState) snapshot() (float64, float64, []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks := make([]Task, len(s.tasks))
	copy(tasks, s.tasks)
	b, _ := json.Marshal(tasks)
	return OverallProgress(s.tasks), s.speed, b
}

func (s *jobState) update(i int, p Progress, done bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 0 || i >= len(s.tasks) {
		return
	}
	s.tasks[i].Progress = p.Fraction
	s.tasks[i].Speed = p.Speed
	s.tasks[i].Done = done
	if done {
		s.tasks[i].Progress = 1
	}
	if p.Speed > 0 {
		s.speed = p.Speed
	}
}

// heartbeat extends the lease on a timer. onStolen fires when the update
// matches zero rows.
//
// The filter includes worker_id, which makes it a fencing check rather than a
// liveness ping: if another worker reclaimed this job because we stalled, our
// UPDATE matches nothing and that IS the signal to stop.
func (w *Worker) heartbeat(ctx context.Context, job *Job, state *jobState, onStolen func()) {
	t := time.NewTicker(w.cfg.HeartbeatEach)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			progress, speed, tasks := state.snapshot()
			ok, err := w.jobs.Heartbeat(ctx, job.ID, w.cfg.WorkerID, w.cfg.LeaseTTL, progress, speed, tasks)
			if err != nil {
				// A transient database error is not proof the lease is gone.
				// Keep going: the lease will expire on its own if we really are
				// partitioned, and the reclaim path handles that correctly.
				w.log.Warn("heartbeat failed", "job_id", job.ID, "error", err)
				continue
			}
			if !ok {
				w.log.Warn("lease lost", "job_id", job.ID)
				onStolen()
				return
			}
		}
	}
}

// runJob is the pipeline: probe -> validate -> ladder -> encode -> upload ->
// master -> ready.
func (w *Worker) runJob(ctx context.Context, job *Job, state *jobState) error {
	// Presigned GET, valid for the whole job. FFmpeg reads it over HTTP and
	// range-requests only what it needs, so a 4 GB source is never downloaded.
	srcURL, err := w.store.PresignedGet(ctx, job.StorageKey, w.cfg.JobTimeout+10*time.Minute)
	if err != nil {
		return fmt.Errorf("presign source: %w", err)
	}

	probe, err := w.prober.Run(ctx, srcURL)
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	state.mu.Lock()
	state.probeRaw = probe.Raw
	state.mu.Unlock()

	// Reject before spending a single CPU-second on encoding.
	if err := probe.Validate(job.Purpose, job.ContentType); err != nil {
		return err
	}

	dispW, dispH := probe.DisplayDimensions()
	ladder := LadderFor(dispH)
	totalMicros := int64(probe.DurationSecs * 1_000_000)

	state.mu.Lock()
	state.tasks = NewTasks(ladder)
	state.mu.Unlock()

	// One temp dir per job, removed unconditionally - including on panic.
	dir, err := os.MkdirTemp(w.cfg.WorkDir, "cf-"+job.ID.String()+"-")
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("temp dir: %w", err)
		}
		if mkErr := os.MkdirAll(w.cfg.WorkDir, 0o755); mkErr != nil {
			return fmt.Errorf("create work dir: %w", mkErr)
		}
		if dir, err = os.MkdirTemp(w.cfg.WorkDir, "cf-"+job.ID.String()+"-"); err != nil {
			return fmt.Errorf("temp dir: %w", err)
		}
	}
	defer os.RemoveAll(dir)

	// --- encode every rung, bounded by the global semaphore ----------------
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	for i, rung := range ladder {
		wg.Add(1)
		go func(i int, rung Rung) {
			defer wg.Done()

			select {
			case w.sem <- struct{}{}:
				defer func() { <-w.sem }()
			case <-ctx.Done():
				return
			}

			outDir := filepath.Join(dir, rung.Name)
			err := w.runner.RunRendition(ctx, srcURL, rung, outDir, totalMicros,
				func(p Progress) { state.update(i, p, false) })
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			state.update(i, Progress{Fraction: 1}, true)
		}(i, rung)
	}
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// --- upload every rendition BEFORE the master --------------------------
	renditions := make([]renditionMeta, 0, len(ladder))
	for _, rung := range ladder {
		prefix := RenditionPrefix(job.AssetID.String(), job.PipelineVersion, rung.Name)
		if err := w.uploadDir(ctx, filepath.Join(dir, rung.Name), prefix); err != nil {
			return fmt.Errorf("upload %s: %w", rung.Name, err)
		}
		renditions = append(renditions, renditionMeta{
			Name:      rung.Name,
			Height:    rung.Height,
			Width:     rung.Width(dispW, dispH),
			Bandwidth: rung.PeakBandwidth(),
			Key:       path.Join(prefix, "index.m3u8"),
		})
	}

	// Poster is best-effort: a film without a thumbnail is still watchable.
	posterKey := ""
	posterPath := filepath.Join(dir, "poster.webp")
	if err := w.runner.ExtractPoster(ctx, srcURL, probe.DurationSecs, posterPath); err != nil {
		w.log.Warn("poster extraction failed", "job_id", job.ID, "error", err)
	} else {
		posterKey = path.Join("renditions", job.AssetID.String(),
			fmt.Sprintf("v%d", job.PipelineVersion), "poster.webp")
		if err := w.uploadFile(ctx, posterPath, posterKey, "image/webp"); err != nil {
			w.log.Warn("poster upload failed", "job_id", job.ID, "error", err)
			posterKey = ""
		}
	}

	// --- master LAST: it is the commit point -------------------------------
	masterPath := filepath.Join(dir, "master.m3u8")
	if err := os.WriteFile(masterPath, BuildMasterPlaylist(ladder, dispW, dispH), 0o644); err != nil {
		return fmt.Errorf("write master: %w", err)
	}
	masterKey := MasterKey(job.AssetID.String(), job.PipelineVersion)
	if err := w.uploadFile(ctx, masterPath, masterKey, "application/vnd.apple.mpegurl"); err != nil {
		return fmt.Errorf("upload master: %w", err)
	}

	rj, _ := json.Marshal(renditions)
	return w.jobs.Succeed(context.WithoutCancel(ctx), job.ID, job.AssetID, w.cfg.WorkerID,
		masterKey, posterKey, rj, probe.Raw,
		probe.DurationSecs, dispW, dispH, probe.Rotation)
}

type renditionMeta struct {
	Name      string `json:"name"`
	Height    int    `json:"height"`
	Width     int    `json:"width"`
	Bandwidth int    `json:"bandwidth"`
	Key       string `json:"key"`
}

func (w *Worker) uploadDir(ctx context.Context, localDir, keyPrefix string) error {
	entries, err := os.ReadDir(localDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ct := "video/mp2t"
		if filepath.Ext(e.Name()) == ".m3u8" {
			ct = "application/vnd.apple.mpegurl"
		}
		if err := w.uploadFile(ctx, filepath.Join(localDir, e.Name()),
			path.Join(keyPrefix, e.Name()), ct); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) uploadFile(ctx context.Context, localPath, key, contentType string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return err
	}
	return w.store.Put(ctx, key, f, st.Size(), contentType)
}

func isReject(err error) bool {
	var re *RejectError
	return errors.As(err, &re)
}
