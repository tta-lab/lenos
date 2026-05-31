package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tta-lab/lenos/internal/csync"
	temenos "github.com/tta-lab/temenos/client"
)

type mockTemenosClient struct {
	mu     sync.Mutex
	jobs   map[string]*temenos.JobInfo
	killed []string
	onKill func(id string)
}

var _ TemenosJobClient = (*mockTemenosClient)(nil)

func newMockTemenosClient() *mockTemenosClient {
	return &mockTemenosClient{jobs: make(map[string]*temenos.JobInfo)}
}

func (m *mockTemenosClient) GetJob(_ context.Context, id string) (*temenos.JobInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.jobs[id]
	if !ok {
		return nil, fmt.Errorf("temenos: job %s not found", id)
	}
	return info, nil
}

func (m *mockTemenosClient) KillJob(_ context.Context, id string) (*temenos.JobInfo, error) {
	if m.onKill != nil {
		m.onKill(id)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.killed = append(m.killed, id)
	info, ok := m.jobs[id]
	if !ok {
		return nil, fmt.Errorf("temenos: job %s not found", id)
	}
	info.Status = "killed"
	return info, nil
}

func (m *mockTemenosClient) setJob(info *temenos.JobInfo) {
	m.mu.Lock()
	m.jobs[info.ID] = info
	m.mu.Unlock()
}

func TestJobWatcher_AddJobWakesLoop(t *testing.T) {
	t.Parallel()
	var received []string
	var mu sync.Mutex
	w := &JobWatcher{
		active:  make(map[string]string),
		notify:  make(chan struct{}, 1),
		enqueue: func(msg string) { mu.Lock(); received = append(received, msg); mu.Unlock() },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	if w.ActiveCount() != 0 {
		t.Fatalf("expected 0 active, got %d", w.ActiveCount())
	}
	w.AddJob("job-1", "echo hello")
	if w.ActiveCount() != 1 {
		t.Fatalf("expected 1 active, got %d", w.ActiveCount())
	}
}

func TestJobWatcher_CompletedJobEnqueuesMessage(t *testing.T) {
	t.Parallel()
	var received []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)
	w := &JobWatcher{
		active:  make(map[string]string),
		notify:  make(chan struct{}, 1),
		enqueue: func(msg string) { mu.Lock(); received = append(received, msg); mu.Unlock(); wg.Done() },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	w.AddJob("job-1", "go test ./...")

	// Simulate the job completing by injecting it directly into the watcher's active map
	// and then triggering a poll. Since we can't inject a real client, we test the
	// formatAndEnqueueCompleted path directly.
	w.formatAndEnqueueCompleted(&temenos.JobInfo{
		ID:       "job-1",
		Status:   "completed",
		ExitCode: 0,
		Stdout:   "ok pkg/foo 0.12s",
	}, "go test ./...")
	w.remove("job-1")

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 message, got %d", len(received))
	}
	if received[0] == "" {
		t.Fatal("expected non-empty message")
	}
	if !strings.Contains(received[0], "background job completed (job_id: job-1)") {
		t.Fatalf("expected completed message to include job id, got %q", received[0])
	}
	if strings.Count(received[0], "job_id") < 2 {
		t.Fatalf("expected completed result body to include job id, got %q", received[0])
	}
	if w.ActiveCount() != 0 {
		t.Fatalf("expected 0 active after remove, got %d", w.ActiveCount())
	}
}

func TestJobWatcher_KilledJobEnqueuesMessage(t *testing.T) {
	t.Parallel()
	var received []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)
	w := &JobWatcher{
		active:  make(map[string]string),
		notify:  make(chan struct{}, 1),
		enqueue: func(msg string) { mu.Lock(); received = append(received, msg); mu.Unlock(); wg.Done() },
	}

	w.formatAndEnqueueKilled(&temenos.JobInfo{
		ID:       "job-2",
		Status:   "killed",
		ExitCode: 137,
	}, "sleep 999")

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 message, got %d", len(received))
	}
	if !strings.Contains(received[0], "background job killed (job_id: job-2)") {
		t.Fatalf("expected killed message to include job id, got %q", received[0])
	}
	if strings.Count(received[0], "job_id") < 2 {
		t.Fatalf("expected killed result body to include job id, got %q", received[0])
	}
}

func TestJobWatcher_ListActiveJobs(t *testing.T) {
	t.Parallel()
	w := &JobWatcher{
		active: map[string]string{
			"job-1": "go test ./...",
			"job-2": "sleep 20",
		},
		notify: make(chan struct{}, 1),
	}

	jobs := w.ListActive()

	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	if jobs[0].ID != "job-1" || jobs[0].Command != "go test ./..." {
		t.Fatalf("unexpected first job: %#v", jobs[0])
	}
	if jobs[1].ID != "job-2" || jobs[1].Command != "sleep 20" {
		t.Fatalf("unexpected second job: %#v", jobs[1])
	}
}

func TestJobWatcher_KillJobKillsRemovesAndEnqueuesRuntimeResult(t *testing.T) {
	t.Parallel()
	mock := newMockTemenosClient()
	mock.setJob(&temenos.JobInfo{ID: "job-1", Status: "running", ExitCode: 137})

	var received []string
	var mu sync.Mutex
	w := &JobWatcher{
		active:  map[string]string{"job-1": "sleep 20"},
		notify:  make(chan struct{}, 1),
		client:  mock,
		enqueue: func(msg string) { mu.Lock(); received = append(received, msg); mu.Unlock() },
	}

	if err := w.KillJob(context.Background(), "job-1"); err != nil {
		t.Fatalf("kill job: %v", err)
	}

	if w.ActiveCount() != 0 {
		t.Fatalf("expected no active jobs, got %d", w.ActiveCount())
	}
	if len(mock.killed) != 1 || mock.killed[0] != "job-1" {
		t.Fatalf("unexpected killed jobs: %#v", mock.killed)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 runtime message, got %d", len(received))
	}
	if received[0] == "" {
		t.Fatal("expected non-empty runtime message")
	}
	if !strings.Contains(received[0], "background job killed (job_id: job-1)") {
		t.Fatalf("expected killed runtime message to include job id, got %q", received[0])
	}
}

func TestJobWatcher_KillJobIgnoresNotFoundAfterJobRemoved(t *testing.T) {
	t.Parallel()
	mock := newMockTemenosClient()
	w := &JobWatcher{
		active: map[string]string{"job-1": "sleep 20"},
		client: mock,
	}
	mock.onKill = func(id string) {
		w.remove(id)
	}

	if err := w.KillJob(context.Background(), "job-1"); err != nil {
		t.Fatalf("expected already-removed job to be treated as done, got %v", err)
	}
	if w.ActiveCount() != 0 {
		t.Fatalf("expected no active jobs, got %d", w.ActiveCount())
	}
}

func TestJobWatcher_KillJobReturnsNotFoundWhenJobStillActive(t *testing.T) {
	t.Parallel()
	mock := newMockTemenosClient()
	w := &JobWatcher{
		active: map[string]string{"job-1": "sleep 20"},
		client: mock,
	}

	if err := w.KillJob(context.Background(), "job-1"); err == nil {
		t.Fatal("expected not found error for job still marked active")
	}
	if w.ActiveCount() != 1 {
		t.Fatalf("expected job to remain active, got %d", w.ActiveCount())
	}
}

func TestSessionAgentStopBackgroundJobsCancelsAndRemovesWatcher(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	a := &sessionAgent{
		jobWatchers: csync.NewMap[string, *sessionJobWatcher](),
	}
	a.jobWatchers.Set("session-1", &sessionJobWatcher{
		watcher: NewJobWatcher(newMockTemenosClient(), "session-1", func(string) {}),
		cancel:  cancel,
	})

	a.stopBackgroundJobs("session-1")

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected watcher context to be canceled")
	}
	if _, ok := a.jobWatchers.Get("session-1"); ok {
		t.Fatal("expected watcher to be removed")
	}
}

func TestJobWatcher_IdleBlocksUntilJobAdded(t *testing.T) {
	t.Parallel()
	mock := newMockTemenosClient()
	var wg sync.WaitGroup
	wg.Add(1)
	w := &JobWatcher{
		active:  make(map[string]string),
		notify:  make(chan struct{}, 1),
		client:  mock,
		enqueue: func(string) { wg.Done() },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	time.Sleep(50 * time.Millisecond)
	if w.ActiveCount() != 0 {
		t.Fatalf("expected 0 active, got %d", w.ActiveCount())
	}

	w.AddJob("job-3", "ls")
	mock.setJob(&temenos.JobInfo{ID: "job-3", Status: "completed"})

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out")
	}
}
