package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRunner(t *testing.T) {
	t.Parallel()

	t.Run("returns SandboxedRunner when Sandbox is true", func(t *testing.T) {
		t.Parallel()
		bg := NewBackgroundRunner(nil)
		call := SessionAgentCall{Sandbox: true}
		r := resolveRunner(call, bg)
		_, isSandboxed := r.(*SandboxedRunner)
		require.True(t, isSandboxed, "should return SandboxedRunner")
		sr := r.(*SandboxedRunner)
		assert.Same(t, bg, sr.bg, "should wire background runner")
	})

	t.Run("returns LocalRunner when Sandbox is false", func(t *testing.T) {
		t.Parallel()
		call := SessionAgentCall{Sandbox: false}
		r := resolveRunner(call, nil)
		_, isLocal := r.(LocalRunner)
		assert.True(t, isLocal, "should return LocalRunner")
	})
}

func TestBackgroundRunnerLifecycle(t *testing.T) {
	// Unit tests for getOrCreateBackgroundRunner / cleanupBackgroundRunner
	// lifecycle: creates on first call, reuses on subsequent calls, cleans up
	// when idle, defers cleanup when jobs are active.

	agent := NewSessionAgent(SessionAgentOptions{})

	t.Run("creates on first call", func(t *testing.T) {
		sa := agent.(*sessionAgent)
		br := sa.getOrCreateBackgroundRunner("s1")
		require.NotNil(t, br)
		assert.Equal(t, 0, br.ActiveCount())
		// Verify stored in bgRunners.
		got, ok := sa.bgRunners.Get("s1")
		require.True(t, ok)
		assert.Same(t, br, got)
	})

	t.Run("reuses on subsequent calls", func(t *testing.T) {
		sa := agent.(*sessionAgent)
		br1 := sa.getOrCreateBackgroundRunner("s2")
		br2 := sa.getOrCreateBackgroundRunner("s2")
		assert.Same(t, br1, br2, "should return the same runner")
	})

	t.Run("cleanup removes idle runner", func(t *testing.T) {
		sa := agent.(*sessionAgent)
		br := sa.getOrCreateBackgroundRunner("s3")
		sa.cleanupBackgroundRunner("s3", br)
		_, ok := sa.bgRunners.Get("s3")
		assert.False(t, ok, "idle runner should be removed")
	})

	t.Run("cleanup defers when jobs active", func(t *testing.T) {
		sa := agent.(*sessionAgent)
		br := sa.getOrCreateBackgroundRunner("s4")

		// Simulate an active job by adding it directly.
		br.mu.Lock()
		br.active["j1"] = &backgroundJob{BackgroundJob: BackgroundJob{ID: "j1", Command: "sleep 99"}}
		br.mu.Unlock()

		sa.cleanupBackgroundRunner("s4", br)
		got, ok := sa.bgRunners.Get("s4")
		assert.True(t, ok, "active runner should stick around")
		assert.Same(t, br, got)
	})
}

func TestEnqueueBackgroundJobResult(t *testing.T) {
	// enqueueBackgroundJobResult returns a func(string) that either queues
	// a runtime call (when session busy) or starts a goroutine to run it.
	t.Run("enqueue creates consumer function", func(t *testing.T) {
		sa := NewSessionAgent(SessionAgentOptions{}).(*sessionAgent)
		fn := sa.enqueueBackgroundJobResult("s1")
		require.NotNil(t, fn)
	})

	t.Run("enqueue when busy appends to queue", func(t *testing.T) {
		sa := NewSessionAgent(SessionAgentOptions{}).(*sessionAgent)
		// Mark session as busy.
		sa.activeRequests.Set("s3", func() {})

		fn := sa.enqueueBackgroundJobResult("s3")
		fn("msg1")
		fn("msg2")

		q, ok := sa.messageQueue.Get("s3")
		require.True(t, ok)
		require.Len(t, q, 2)
		assert.Equal(t, "msg1", q[0].Prompt)
		assert.Equal(t, "msg2", q[1].Prompt)
		assert.True(t, q[0].runtimePrompt)
		assert.Equal(t, "s3", q[0].SessionID)
	})
}
