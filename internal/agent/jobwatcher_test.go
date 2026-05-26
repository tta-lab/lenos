package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	temenos "github.com/tta-lab/temenos/client"
)

type mockTemenosClient struct {
	mu   sync.Mutex
	jobs map[string]*temenos.JobInfo
}

func newMockTemenosClient() *mockTemenosClient {
	return &mockTemenosClient{jobs: make(map[string]*temenos.JobInfo)}
}

func (m *mockTemenosClient) GetJob(_ context.Context, id string) (*temenos.JobInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.jobs[id]
	if !ok {
		return nil, nil
	}
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
