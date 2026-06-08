package agent

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tta-lab/lenos/internal/agent/lenosbash"
	"github.com/tta-lab/lenos/internal/message"
)

// BackgroundJob represents one tracked background command.
type BackgroundJob struct {
	ID      string
	Command string
}

// backgroundJob tracks the running state of one background command.
type backgroundJob struct {
	BackgroundJob
	cancel context.CancelFunc
}

// BackgroundRunner tracks in-flight background commands. When a goroutine
// completes, it calls the enqueue callback so the session agent can inject
// a runtime notification into the model stream.
type BackgroundRunner struct {
	mu         sync.Mutex
	active     map[string]*backgroundJob
	idleCh     chan struct{}
	idleClosed bool
	enqueue    func(msg string)
	onIdle     func()
	onIdleMu   sync.Mutex

	// completions stores formatted runtime prompts for completed
	// background jobs since the last drain. WaitAndDrain returns
	// them and clears the store. Callers append them directly to
	// the model stream — no dependency on external drain queues.
	completions []turnPrompt
}

// NewBackgroundRunner creates a dormant runner. Track must be called to
// register jobs; the runner is passive — goroutines call back when done.
func NewBackgroundRunner(enqueue func(msg string)) *BackgroundRunner {
	idleCh := make(chan struct{})
	close(idleCh)
	return &BackgroundRunner{
		active:     make(map[string]*backgroundJob),
		idleCh:     idleCh,
		idleClosed: true,
		enqueue:    enqueue,
	}
}

// Track registers a running background command. When the command completes
// (or is cancelled), the runner formats the result and calls enqueue.
// The caller must start the goroutine before calling Track.
func (r *BackgroundRunner) Track(jobID, command string, cancel context.CancelFunc, resultCh <-chan backgroundResult) {
	r.mu.Lock()
	if len(r.active) == 0 {
		r.idleCh = make(chan struct{})
		r.idleClosed = false
	}
	r.active[jobID] = &backgroundJob{
		BackgroundJob: BackgroundJob{ID: jobID, Command: command},
		cancel:        cancel,
	}
	r.mu.Unlock()

	go func() {
		result, ok := <-resultCh
		if !ok {
			return
		}

		// Build the completion prompt before taking the lock so the
		// formatted text is ready and the critical section is short.
		var promptText string
		if result.killed {
			promptText = formatKilledPrompt(jobID, command, result.exitCode, result.duration)
		} else {
			promptText = formatCompletedPrompt(jobID, command, result.stdout, result.stderr, result.exitCode, result.err, result.duration)
		}

		r.mu.Lock()
		_, stillActive := r.active[jobID]
		delete(r.active, jobID)
		idle := len(r.active) == 0
		// Store the completion atomically with the active set so
		// WaitAndDrain sees it with no race window.
		if stillActive && !result.killed {
			r.completions = append(r.completions, turnPrompt{
				Text:    promptText,
				Persist: true,
				Role:    message.Runtime,
			})
		}
		r.mu.Unlock()

		// If the job was already removed by StopAll or KillJob, skip
		// enqueue: the runner no longer owns this job.  Completions
		// stored under lock are discarded since they won't be
		// drained by WaitAndDrain.
		if !stillActive {
			if idle {
				r.finishIdle()
			}
			return
		}

		// Killed jobs need legacy enqueue (not stored in completions).
		// Completed jobs are delivered via WaitAndDrain — do NOT
		// also enqueue, or the active loop will see duplicates.
		if result.killed && r.enqueue != nil {
			r.enqueue(promptText)
		}

		if idle {
			r.finishIdle()
		}
	}()
}

type backgroundResult struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
	killed   bool
	duration time.Duration
}

// ActiveCount returns the number of tracked background jobs.
func (r *BackgroundRunner) ActiveCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.active)
}

// WaitIdle blocks until all tracked background jobs finish or ctx is canceled.
func (r *BackgroundRunner) WaitIdle(ctx context.Context) error {
	r.mu.Lock()
	if len(r.active) == 0 {
		r.mu.Unlock()
		return nil
	}
	idleCh := r.idleCh
	r.mu.Unlock()

	select {
	case <-idleCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *BackgroundRunner) finishIdle() {
	r.mu.Lock()
	if !r.idleClosed {
		close(r.idleCh)
		r.idleClosed = true
	}
	r.mu.Unlock()

	r.onIdleMu.Lock()
	f := r.onIdle
	r.onIdle = nil
	r.onIdleMu.Unlock()
	if f != nil {
		f()
	}
}

// WaitAndDrain blocks until all tracked background jobs finish (or ctx
// is canceled), then returns the number of completions that were enqueued
// since the last drain. After the call, the completion counter is reset
// to zero. This gives the loop a deterministic signal: if completions &gt; 0,
// the turn must continue.
func (r *BackgroundRunner) WaitAndDrain(ctx context.Context) []turnPrompt {
	r.mu.Lock()
	if len(r.active) == 0 {
		out := r.completions
		r.completions = nil
		r.mu.Unlock()
		return out
	}
	idleCh := r.idleCh
	r.mu.Unlock()

	select {
	case <-idleCh:
	case <-ctx.Done():
	}

	r.mu.Lock()
	out := r.completions
	r.completions = nil
	r.mu.Unlock()
	return out
}

// CompletionCount returns the number of completions enqueued since the
// last drain. Safe to call while active jobs are still running.
func (r *BackgroundRunner) CompletionCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.completions)
}

// ListActive returns all tracked background jobs sorted by ID.
func (r *BackgroundRunner) ListActive() []BackgroundJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	jobs := make([]BackgroundJob, 0, len(r.active))
	for _, j := range r.active {
		jobs = append(jobs, j.BackgroundJob)
	}
	slices.SortFunc(jobs, func(a, b BackgroundJob) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return jobs
}

// KillJob cancels the context of a running background job. The goroutine
// will detect the cancellation, set killed=true on the result, and the
// Track watcher will enqueue a killed notification.
func (r *BackgroundRunner) KillJob(jobID string) error {
	r.mu.Lock()
	job, ok := r.active[jobID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("background job %s is not active", jobID)
	}
	job.cancel()
	return nil
}

// StopAll cancels and removes all running background jobs.
func (r *BackgroundRunner) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.active) == 0 {
		r.completions = nil
		return
	}
	for id, job := range r.active {
		job.cancel()
		delete(r.active, id)
	}
	if !r.idleClosed {
		close(r.idleCh)
		r.idleClosed = true
	}
	r.completions = nil
}

// newJobID generates a short hex ID. Uses time-based prefix for readability.
func newJobID() string {
	return uuid.New().String()[:8]
}

func formatCompletedPrompt(jobID, command, stdout, stderr string, exitCode int, runErr error, elapsed time.Duration) string {
	if runErr != nil {
		result := lenosbash.ResultBlock(fmt.Sprintf(
			"job_id: %s\ncommand: %s\nerror: %v\nwall_time: %s",
			jobID, command, runErr, elapsed.Truncate(time.Second),
		))
		obs := fmt.Sprintf("background job completed (job_id: %s)\n\n%s", jobID, result)
		return lenosbash.RuntimeBlock(obs)
	}
	result := lenosbash.ResultBlock(fmt.Sprintf(
		"job_id: %s\ncommand: %s\nexit_code: %d\nwall_time: %s\nstdout: %s\nstderr: %s",
		jobID, command, exitCode, elapsed.Truncate(time.Second), stdout, stderr,
	))
	obs := fmt.Sprintf("background job completed (job_id: %s)\n\n%s", jobID, result)
	return lenosbash.RuntimeBlock(obs)
}

func formatKilledPrompt(jobID, command string, exitCode int, elapsed time.Duration) string {
	result := lenosbash.ResultBlock(fmt.Sprintf(
		"job_id: %s\ncommand: %s\nexit_code: %d\nwall_time: %s",
		jobID, command, exitCode, elapsed.Truncate(time.Second),
	))
	obs := fmt.Sprintf("background job killed (job_id: %s)\n\n%s", jobID, result)
	return lenosbash.RuntimeBlock(obs)
}
