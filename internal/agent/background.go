package agent

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/google/uuid"
	"github.com/tta-lab/lenos/internal/agent/lenosbash"
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
	mu       sync.Mutex
	active   map[string]*backgroundJob
	enqueue  func(msg string)
	onIdle   func()
	onIdleMu sync.Mutex
}

// NewBackgroundRunner creates a dormant runner. Track must be called to
// register jobs; the runner is passive — goroutines call back when done.
func NewBackgroundRunner(enqueue func(msg string)) *BackgroundRunner {
	return &BackgroundRunner{
		active:  make(map[string]*backgroundJob),
		enqueue: enqueue,
	}
}

// Track registers a running background command. When the command completes
// (or is cancelled), the runner formats the result and calls enqueue.
// The caller must start the goroutine before calling Track.
func (r *BackgroundRunner) Track(jobID, command string, cancel context.CancelFunc, resultCh <-chan backgroundResult) {
	r.mu.Lock()
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

		r.mu.Lock()
		_, stillActive := r.active[jobID]
		delete(r.active, jobID)
		idle := len(r.active) == 0
		r.mu.Unlock()

		// If the job was already removed by StopAll or KillJob, skip
		// enqueue: the runner no longer owns this job.
		if !stillActive {
			if idle {
				r.onIdleMu.Lock()
				f := r.onIdle
				r.onIdle = nil
				r.onIdleMu.Unlock()
				if f != nil {
					f()
				}
			}
			return
		}

		if result.killed {
			r.formatAndEnqueueKilled(jobID, command, result.exitCode)
		} else {
			r.formatAndEnqueueCompleted(jobID, command, result.stdout, result.stderr, result.exitCode, result.err)
		}

		if idle {
			r.onIdleMu.Lock()
			f := r.onIdle
			r.onIdle = nil
			r.onIdleMu.Unlock()
			if f != nil {
				f()
			}
		}
	}()
}

type backgroundResult struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
	killed   bool
}

// ActiveCount returns the number of tracked background jobs.
func (r *BackgroundRunner) ActiveCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.active)
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

// KillJob cancels the context of a running background job and removes it
// from tracking. The goroutine will still see the result on resultCh but
// skip enqueue (stillActive will be false).
func (r *BackgroundRunner) KillJob(jobID string) error {
	r.mu.Lock()
	job, ok := r.active[jobID]
	if ok {
		delete(r.active, jobID)
	}
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
	for id, job := range r.active {
		job.cancel()
		delete(r.active, id)
	}
}

func (r *BackgroundRunner) formatAndEnqueueCompleted(jobID, command, stdout, stderr string, exitCode int, runErr error) {
	if r.enqueue == nil {
		return
	}
	if runErr != nil {
		result := lenosbash.ResultBlock(fmt.Sprintf(
			"job_id: %s\ncommand: %s\nerror: %v",
			jobID, command, runErr,
		))
		obs := fmt.Sprintf("background job completed (job_id: %s)\n\n%s", jobID, result)
		r.enqueue(lenosbash.RuntimeBlock(obs))
		return
	}
	result := lenosbash.ResultBlock(fmt.Sprintf(
		"job_id: %s\ncommand: %s\nexit_code: %d\nstdout: %s\nstderr: %s",
		jobID, command, exitCode, stdout, stderr,
	))
	obs := fmt.Sprintf("background job completed (job_id: %s)\n\n%s", jobID, result)
	r.enqueue(lenosbash.RuntimeBlock(obs))
}

func (r *BackgroundRunner) formatAndEnqueueKilled(jobID, command string, exitCode int) {
	if r.enqueue == nil {
		return
	}
	result := lenosbash.ResultBlock(fmt.Sprintf(
		"job_id: %s\ncommand: %s\nexit_code: %d",
		jobID, command, exitCode,
	))
	obs := fmt.Sprintf("background job killed (job_id: %s)\n\n%s", jobID, result)
	r.enqueue(lenosbash.RuntimeBlock(obs))
}

// newJobID generates a short hex ID. Uses time-based prefix for readability.
func newJobID() string {
	return uuid.New().String()[:8]
}
