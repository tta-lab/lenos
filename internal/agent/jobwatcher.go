package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	temenos "github.com/tta-lab/temenos/client"
)

const jobPollInterval = 10 * time.Second

// JobWatcher polls the temenos job socket for completed background jobs
// and enqueues <-Runtime notifications into the session messageQueue.
// It blocks on a channel when idle — zero temenos traffic until a job
// enters background.
type JobWatcher struct {
	mu        sync.Mutex
	active    map[string]string // job_id → original command
	notify    chan struct{}
	client    *temenos.Client
	enqueue   func(msg string)
	sessionID string
}

// NewJobWatcher creates a dormant watcher. Call Run in a goroutine to start.
func NewJobWatcher(c *temenos.Client, sessionID string, enqueue func(msg string)) *JobWatcher {
	return &JobWatcher{
		active:    make(map[string]string),
		notify:    make(chan struct{}, 1),
		client:    c,
		enqueue:   enqueue,
		sessionID: sessionID,
	}
}

// AddJob registers a background job for polling and wakes the watcher.
func (w *JobWatcher) AddJob(jobID, command string) {
	w.mu.Lock()
	w.active[jobID] = command
	w.mu.Unlock()

	select {
	case w.notify <- struct{}{}:
	default:
	}
}

// ActiveCount returns the number of tracked background jobs.
func (w *JobWatcher) ActiveCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.active)
}

// Run blocks when idle and polls when jobs are active. Run in a goroutine;
// cancel ctx to stop.
func (w *JobWatcher) Run(ctx context.Context) {
	for {
		select {
		case <-w.notify:
		case <-ctx.Done():
			return
		}

		for {
			w.mu.Lock()
			if len(w.active) == 0 {
				w.mu.Unlock()
				break
			}
			w.mu.Unlock()

			select {
			case <-ctx.Done():
				return
			case <-time.After(jobPollInterval):
			}

			w.pollAll(ctx)
		}
	}
}

func (w *JobWatcher) pollAll(ctx context.Context) {
	w.mu.Lock()
	ids := make(map[string]string, len(w.active))
	for id, cmd := range w.active {
		ids[id] = cmd
	}
	w.mu.Unlock()

	for id, cmd := range ids {
		info, err := w.client.GetJob(ctx, id)
		if err != nil {
			slog.Warn("JobWatcher: poll failed", "job_id", id, "error", err)
			continue
		}
		if info == nil {
			continue
		}

		switch info.Status {
		case "completed":
			w.formatAndEnqueueCompleted(info, cmd)
			w.remove(id)
		case "killed":
			w.formatAndEnqueueKilled(info, cmd)
			w.remove(id)
		}
	}
}

func (w *JobWatcher) remove(jobID string) {
	w.mu.Lock()
	delete(w.active, jobID)
	w.mu.Unlock()
}

func (w *JobWatcher) formatAndEnqueueCompleted(info *temenos.JobInfo, command string) {
	obs := fmt.Sprintf(
		"<-Runtime background job completed (job_id: %s)\n\n<result>\ncommand: %s\nexit_code: %d\nstdout: %s\nstderr: %s\n</result>",
		info.ID, command, info.ExitCode, info.Stdout, info.Stderr,
	)
	w.enqueue(obs)
}

func (w *JobWatcher) formatAndEnqueueKilled(info *temenos.JobInfo, command string) {
	obs := fmt.Sprintf(
		"<-Runtime background job killed (job_id: %s)\n\n<result>\ncommand: %s\nexit_code: %d\n</result>",
		info.ID, command, info.ExitCode,
	)
	w.enqueue(obs)
}
