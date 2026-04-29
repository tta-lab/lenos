package agent

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/temenos/client"
)

func TestLocalRunner_Success(t *testing.T) {
	t.Parallel()
	res := LocalRunner{}.Run(context.Background(), `echo hello`, nil, nil)
	require.NoError(t, res.Err)
	assert.Equal(t, 0, res.ExitCode)
	assert.Equal(t, "hello\n", string(res.Stdout))
	assert.Empty(t, string(res.Stderr))
}

func TestLocalRunner_NonZeroExit(t *testing.T) {
	t.Parallel()
	res := LocalRunner{}.Run(context.Background(), `exit 7`, nil, nil)
	require.NoError(t, res.Err)
	assert.Equal(t, 7, res.ExitCode)
}

func TestLocalRunner_StderrCaptured(t *testing.T) {
	t.Parallel()
	res := LocalRunner{}.Run(context.Background(), `echo err 1>&2; exit 3`, nil, nil)
	require.NoError(t, res.Err)
	assert.Equal(t, 3, res.ExitCode)
	assert.Equal(t, "err\n", string(res.Stderr))
}

func TestLocalRunner_Timeout(t *testing.T) {
	t.Parallel()
	// Override the per-cmd timeout for this test to avoid a 120s wait.
	// We do this by giving the parent context a shorter deadline; the runner
	// honours the tighter of the two.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	res := LocalRunner{}.Run(ctx, `sleep 5`, nil, nil)
	elapsed := time.Since(start)

	require.Error(t, res.Err)
	assert.True(t, errors.Is(res.Err, context.DeadlineExceeded), "want DeadlineExceeded, got %v", res.Err)
	assert.Equal(t, -1, res.ExitCode)
	assert.Less(t, elapsed, 2*time.Second, "timeout should fire near deadline")
}

func TestLocalRunner_ContextCancelMidExec(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	res := LocalRunner{}.Run(ctx, `sleep 5`, nil, nil)
	require.Error(t, res.Err)
	assert.True(t, errors.Is(res.Err, context.Canceled), "want Canceled, got %v", res.Err)
	assert.Equal(t, -1, res.ExitCode)
}

// TestLocalRunner_EnvMergeAndOverride locks the env-merging contract:
//  1. Parent process env (PATH, HOME, …) IS inherited by the subprocess.
//  2. Explicit map entries override parent values for the same key.
//  3. New keys from the explicit map are added.
func TestLocalRunner_EnvMergeAndOverride(t *testing.T) {
	t.Parallel()

	// PATH must be inherited so /bin/bash, /bin/echo, etc. resolve.
	require.NotEmpty(t, os.Getenv("PATH"))

	overrides := map[string]string{
		"LENOS_SESSION_ID": "sess-123",
		"LENOS_DATA_DIR":   "/tmp/lenos-test",
		"PATH":             "/override-path", // explicit override
	}

	// Probe all three env vars; rely on /usr/bin/printenv being on PATH (it
	// is a separate binary), which we get because /bin/bash resolves via the
	// real PATH inherited at exec time.
	res := LocalRunner{}.Run(context.Background(),
		`echo "SESSION=$LENOS_SESSION_ID"; echo "DATA=$LENOS_DATA_DIR"; echo "PATH=$PATH"; echo "HOME_PRESENT=${HOME:+yes}"`,
		overrides, nil)
	require.NoError(t, res.Err)
	require.Equal(t, 0, res.ExitCode, "stderr=%q", string(res.Stderr))

	out := string(res.Stdout)
	assert.Contains(t, out, "SESSION=sess-123")
	assert.Contains(t, out, "DATA=/tmp/lenos-test")
	assert.Contains(t, out, "PATH=/override-path", "explicit map should override parent PATH")
	assert.Contains(t, out, "HOME_PRESENT=yes", "parent HOME should be inherited")
}

func TestLocalRunner_AllowedPathsFirstAsCwd(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	res := LocalRunner{}.Run(context.Background(), `pwd`, nil,
		[]client.AllowedPath{{Path: tmp}})
	require.NoError(t, res.Err)
	require.Equal(t, 0, res.ExitCode)
	// macOS resolves /var to /private/var; trim either prefix for the check.
	got := strings.TrimSpace(string(res.Stdout))
	assert.True(t, strings.HasSuffix(got, tmp) || got == tmp,
		"want pwd to resolve to (or under) %q, got %q", tmp, got)
}
