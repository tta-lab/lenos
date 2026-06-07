package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackgroundRunner_TrackCompletion(t *testing.T) {
	br := NewBackgroundRunner(nil)

	resultCh := make(chan backgroundResult, 1)
	br.Track("abc123", "sleep 1", func() {}, resultCh)

	resultCh <- backgroundResult{
		stdout:   "hello",
		stderr:   "",
		exitCode: 0,
	}

	// Completion delivered via WaitAndDrain, not enqueue callback.
	prompts := br.WaitAndDrain(context.Background())
	require.Len(t, prompts, 1, "one completion prompt")
	assert.Contains(t, prompts[0].Text, "background job completed")
	assert.Contains(t, prompts[0].Text, "abc123")
	assert.Contains(t, prompts[0].Text, "hello")

	assert.Equal(t, 0, br.ActiveCount())
	assert.Empty(t, br.ListActive())
}

func TestBackgroundRunner_TrackKilled(t *testing.T) {
	receivedCh := make(chan string, 1)
	br := NewBackgroundRunner(func(msg string) {
		receivedCh <- msg
	})

	resultCh := make(chan backgroundResult, 1)
	cancel := func() {}
	br.Track("xyz789", "long build", cancel, resultCh)

	resultCh <- backgroundResult{
		exitCode: -1,
		killed:   true,
	}

	received := <-receivedCh
	require.NotEmpty(t, received, "enqueue callback should have fired")
	assert.Contains(t, received, "background job killed")
	assert.Contains(t, received, "xyz789")
}

func TestBackgroundRunner_KillJobCancelsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	resultCh := make(chan backgroundResult, 1)
	br := NewBackgroundRunner(nil)
	br.Track("killme", "forever", cancel, resultCh)

	err := br.KillJob("killme")
	require.NoError(t, err)

	// The context should be canceled.
	select {
	case <-ctx.Done():
		// expected
	case <-time.After(time.Second):
		t.Fatal("context should have been canceled by KillJob")
	}

	// Close the channel to unblock the Track goroutine.
	close(resultCh)
}

func TestBackgroundRunner_KillJobUnknown(t *testing.T) {
	br := NewBackgroundRunner(nil)
	err := br.KillJob("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not active")
}

func TestBackgroundRunner_StopAll(t *testing.T) {
	_, cancel1 := context.WithCancel(context.Background())
	_, cancel2 := context.WithCancel(context.Background())

	br := NewBackgroundRunner(nil)
	ch1 := make(chan backgroundResult, 1)
	ch2 := make(chan backgroundResult, 1)
	br.Track("job1", "cmd1", cancel1, ch1)
	br.Track("job2", "cmd2", cancel2, ch2)

	assert.Equal(t, 2, br.ActiveCount())
	assert.Len(t, br.ListActive(), 2)

	br.StopAll()
	// Both jobs removed (no goroutine to fire onIdle since no results sent)
	assert.Equal(t, 0, br.ActiveCount())
}

func TestBackgroundRunner_OnIdle(t *testing.T) {
	idleCh := make(chan struct{}, 1)
	br := NewBackgroundRunner(nil)
	setOnIdle(br, func() {
		idleCh <- struct{}{}
	})

	resultCh := make(chan backgroundResult, 1)
	br.Track("job", "cmd", func() {}, resultCh)

	resultCh <- backgroundResult{exitCode: 0}

	select {
	case <-idleCh:
		// onIdle fired
	case <-time.After(time.Second):
		t.Fatal("onIdle should fire when last job completes")
	}
}

func TestBackgroundRunner_OnIdleSetToNil(t *testing.T) {
	// Regression: setting onIdle to nil must not crash the Track goroutine.
	br := NewBackgroundRunner(nil)
	setOnIdle(br, nil) // cleared before any jobs complete

	resultCh := make(chan backgroundResult, 1)
	br.Track("job", "cmd", func() {}, resultCh)

	resultCh <- backgroundResult{exitCode: 0}

	// Give the goroutine time to process.
	time.Sleep(50 * time.Millisecond)
	// Should not panic; ActiveCount confirms job was consumed.
	assert.Equal(t, 0, br.ActiveCount())
}

func TestBackgroundRunner_WaitAndDrain_BlocksUntilIdle(t *testing.T) {
	t.Parallel()
	br := NewBackgroundRunner(func(msg string) {})

	resultCh := make(chan backgroundResult, 1)
	br.Track("job-1", "sleep 1", func() {}, resultCh)

	done := make(chan []turnPrompt, 1)
	go func() {
		done <- br.WaitAndDrain(context.Background())
	}()

	// Should still be waiting.
	select {
	case <-done:
		t.Fatal("WaitAndDrain should not return while active")
	case <-time.After(50 * time.Millisecond):
	}

	// Deliver result.
	resultCh <- backgroundResult{stdout: "ok", exitCode: 0}

	select {
	case prompts := <-done:
		assert.Len(t, prompts, 1, "one completion prompt")
	case <-time.After(time.Second):
		t.Fatal("WaitAndDrain should return after completion")
	}
	assert.Equal(t, 0, br.ActiveCount())
}

func TestBackgroundRunner_WaitAndDrain_IdleImmediate(t *testing.T) {
	t.Parallel()
	br := NewBackgroundRunner(func(msg string) {})

	// Idle runner returns empty slice immediately.
	assert.Empty(t, br.WaitAndDrain(context.Background()))

	// Completions store resets on every drain.
	resultCh := make(chan backgroundResult, 1)
	br.Track("job-1", "cmd", func() {}, resultCh)
	resultCh <- backgroundResult{stdout: "ok", exitCode: 0}

	assert.Len(t, br.WaitAndDrain(context.Background()), 1)

	assert.Empty(t, br.WaitAndDrain(context.Background()), "drain resets store")
}

func TestBackgroundRunner_WaitAndDrain_ContextCanceled(t *testing.T) {
	t.Parallel()
	br := NewBackgroundRunner(func(msg string) {})

	resultCh := make(chan backgroundResult, 1)
	br.Track("job-1", "forever", func() {}, resultCh)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	prompts := br.WaitAndDrain(ctx)
	// Context already canceled so WaitAndDrain returns without waiting.
	assert.Empty(t, prompts)
}

func TestBackgroundRunner_CompletionCount_NonBlocking(t *testing.T) {
	t.Parallel()
	br := NewBackgroundRunner(func(msg string) {})

	resultCh := make(chan backgroundResult, 1)
	br.Track("job-1", "cmd", func() {}, resultCh)

	// CompletionCount returns 0 while job is still active.
	assert.Equal(t, 0, br.CompletionCount())

	resultCh <- backgroundResult{stdout: "ok", exitCode: 0}
	time.Sleep(20 * time.Millisecond) // let goroutine process

	assert.Equal(t, 1, br.CompletionCount())

	// Drain resets it.
	br.WaitAndDrain(context.Background())
	assert.Equal(t, 0, br.CompletionCount())
}

func TestBackgroundRunner_StopAllResetsCompleted(t *testing.T) {
	t.Parallel()
	_, cancel1 := context.WithCancel(context.Background())
	_, cancel2 := context.WithCancel(context.Background())

	br := NewBackgroundRunner(func(msg string) {})
	ch1 := make(chan backgroundResult, 1)
	ch2 := make(chan backgroundResult, 1)
	br.Track("job-1", "cmd1", cancel1, ch1)
	br.Track("job-2", "cmd2", cancel2, ch2)

	// Deliver one result before StopAll.
	ch1 <- backgroundResult{stdout: "ok", exitCode: 0}
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 1, br.CompletionCount())

	br.StopAll()
	assert.Equal(t, 0, br.CompletionCount())
	assert.Equal(t, 0, br.ActiveCount())
}

func TestBackgroundRunner_KillJobNoCompletedIncrement(t *testing.T) {
	t.Parallel()
	_, cancel := context.WithCancel(context.Background())
	br := NewBackgroundRunner(func(msg string) {})
	ch := make(chan backgroundResult, 1)
	br.Track("job-1", "cmd", cancel, ch)

	br.KillJob("job-1")
	// The cancel fires, goroutine sends killed result.
	ch <- backgroundResult{killed: true}

	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, br.CompletionCount(), "killed jobs do not increment completed")
}
