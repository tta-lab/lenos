package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/temenos/sandbox"
)

// hasSandbox returns true if sandbox-exec (darwin) or bwrap (linux) is available.
func hasSandbox(t *testing.T) bool {
	t.Helper()
	cfg, sbx, err := sandbox.LoadConfig("")
	if err != nil || cfg == nil || sbx == nil {
		return false
	}
	return sbx.IsAvailable()
}

func setAutoBackgroundAfterForTest(t *testing.T, d time.Duration) {
	t.Helper()
	prev := defaultAutoBackgroundAfter
	defaultAutoBackgroundAfter = d
	t.Cleanup(func() {
		defaultAutoBackgroundAfter = prev
	})
}

func TestSandboxedRunner_SynchronousCompletion(t *testing.T) {
	if !hasSandbox(t) {
		t.Skip("sandbox not available")
	}

	runner := &SandboxedRunner{}
	res := runner.Run(
		context.Background(),
		"echo hello",
		map[string]string{"PATH": "/usr/bin:/bin"},
		[]AllowedPath{{Path: t.TempDir()}},
	)

	require.NoError(t, res.Err)
	assert.Equal(t, 0, res.ExitCode)
	assert.Contains(t, string(res.Stdout), "hello")
	assert.False(t, res.Background)
	assert.NotZero(t, res.Duration)
}

func TestSandboxedRunner_NonZeroExit(t *testing.T) {
	if !hasSandbox(t) {
		t.Skip("sandbox not available")
	}

	runner := &SandboxedRunner{}
	res := runner.Run(
		context.Background(),
		"exit 42",
		map[string]string{"PATH": "/usr/bin:/bin"},
		[]AllowedPath{{Path: t.TempDir()}},
	)

	// Non-zero exit is not an Err — it's reflected in ExitCode.
	assert.NoError(t, res.Err)
	assert.Equal(t, 42, res.ExitCode)
}

func TestSandboxedRunner_StderrCaptured(t *testing.T) {
	if !hasSandbox(t) {
		t.Skip("sandbox not available")
	}

	runner := &SandboxedRunner{}
	res := runner.Run(
		context.Background(),
		"echo stderr output >&2",
		map[string]string{"PATH": "/usr/bin:/bin"},
		[]AllowedPath{{Path: t.TempDir()}},
	)

	require.NoError(t, res.Err)
	assert.Contains(t, string(res.Stderr), "stderr output")
}

func TestSandboxedRunner_AutoBackground(t *testing.T) {
	if !hasSandbox(t) {
		t.Skip("sandbox not available")
	}
	setAutoBackgroundAfterForTest(t, 20*time.Millisecond)

	// Use a BackgroundRunner that captures the enqueued message.
	var bgMsg string
	bg := NewBackgroundRunner(func(msg string) {
		bgMsg = msg
	})

	runner := &SandboxedRunner{bg: bg}
	res := runner.Run(
		context.Background(),
		// Loop beyond the auto-background threshold to trigger background handoff.
		"while true; do :; done",
		map[string]string{"PATH": "/usr/bin:/bin"},
		[]AllowedPath{{Path: t.TempDir()}},
	)

	require.NoError(t, res.Err)
	assert.True(t, res.Background, "long command should go to background")
	assert.NotEmpty(t, res.JobID)
	assert.Equal(t, 1, bg.ActiveCount())

	// Kill the background job so it doesn't linger.
	err := bg.KillJob(res.JobID)
	require.NoError(t, err)

	// Wait for the enqueue callback to fire.
	for i := 0; i < 100 && bgMsg == ""; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	assert.Contains(t, bgMsg, "background job killed")
	assert.Contains(t, bgMsg, res.JobID)
}

func TestSandboxedRunner_NoBackgroundRunner_WaitsForCompletion(t *testing.T) {
	if !hasSandbox(t) {
		t.Skip("sandbox not available")
	}
	setAutoBackgroundAfterForTest(t, time.Nanosecond)

	// No BackgroundRunner — the runner should wait for the command to finish,
	// not return Background: true.
	runner := &SandboxedRunner{bg: nil}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use a command that exceeds the auto-background threshold but completes.
	res := runner.Run(
		ctx,
		"i=0; while [ $i -lt 200000 ]; do i=$((i+1)); done; echo done",
		map[string]string{"PATH": "/usr/bin:/bin"},
		[]AllowedPath{{Path: t.TempDir()}},
	)

	require.NoError(t, res.Err)
	assert.False(t, res.Background, "without BackgroundRunner, should wait synchronously")
	assert.Contains(t, string(res.Stdout), "done")
	assert.NotZero(t, res.Duration)
}

func TestSandboxedRunner_ContextCancel(t *testing.T) {
	if !hasSandbox(t) {
		t.Skip("sandbox not available")
	}

	ctx, cancel := context.WithCancel(context.Background())

	runner := &SandboxedRunner{bg: nil}

	// Cancel the context before the command finishes.
	done := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
		close(done)
	}()

	res := runner.Run(
		ctx,
		"while true; do :; done",
		map[string]string{"PATH": "/usr/bin:/bin"},
		[]AllowedPath{{Path: t.TempDir()}},
	)

	// Should get a context cancellation error.
	assert.Error(t, res.Err)
	<-done
}

func TestSandboxedRunner_AllowedPathsAsWorkDir(t *testing.T) {
	if !hasSandbox(t) {
		t.Skip("sandbox not available")
	}

	tmpDir := t.TempDir()
	runner := &SandboxedRunner{}
	res := runner.Run(
		context.Background(),
		"pwd",
		map[string]string{"PATH": "/usr/bin:/bin"},
		[]AllowedPath{{Path: tmpDir}},
	)

	require.NoError(t, res.Err)
	// macOS resolves /var to /private/var; trim either for the check.
	got := strings.TrimPrefix(strings.TrimSpace(string(res.Stdout)), "/private")
	want := strings.TrimPrefix(tmpDir, "/private")
	assert.Equal(t, want, got)
}

func TestSandboxedRunner_ConfigReuse(t *testing.T) {
	if !hasSandbox(t) {
		t.Skip("sandbox not available")
	}

	// First Run() loads config lazily. Second Run() reuses it.
	runner := &SandboxedRunner{}

	res1 := runner.Run(
		context.Background(),
		"echo first",
		map[string]string{"PATH": "/usr/bin:/bin"},
		[]AllowedPath{{Path: t.TempDir()}},
	)
	require.NoError(t, res1.Err)
	assert.Contains(t, string(res1.Stdout), "first", "first call should succeed")

	res2 := runner.Run(
		context.Background(),
		"echo second",
		map[string]string{"PATH": "/usr/bin:/bin"},
		[]AllowedPath{{Path: t.TempDir()}},
	)
	require.NoError(t, res2.Err)
	assert.Contains(t, string(res2.Stdout), "second", "second call should reuse loaded config")
	assert.NotNil(t, runner.sbx, "sandbox should be cached after first Run")
	assert.NotNil(t, runner.cfg, "config should be cached after first Run")
}
