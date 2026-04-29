package agent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"time"

	"github.com/tta-lab/temenos/client"
)

// DefaultPerCmdTimeout caps a single bash subprocess. Matches the pre-existing
// temenos sandbox default. Agents can override via bash-native `timeout 30m`.
const DefaultPerCmdTimeout = 120 * time.Second

// ExecResult is the outcome of running one agent emit through a Runner.
//
//   - ExitCode is the subprocess exit code on normal exit, -1 on runner-level
//     failure (timeout, missing binary, sandbox daemon error).
//   - Err is non-nil only on runner-level failures, NOT on non-zero exit. A
//     timeout sets Err to context.DeadlineExceeded so the loop can branch on it.
type ExecResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Duration time.Duration
	Err      error
}

// Runner abstracts the execution backend (local subprocess or temenos sandbox).
// The single method keeps the interface trivial; tests can pass a fake.
type Runner interface {
	Run(ctx context.Context, bash string, env map[string]string, allowedPaths []client.AllowedPath) ExecResult
}

// LocalRunner runs commands via /bin/bash -c on the host.
//
// Env handling: the subprocess inherits the parent process environment
// (os.Environ()) and overlays the explicit env map on top. This preserves
// PATH/HOME/TMPDIR while letting the loop set LENOS_* and other per-call
// values. If a key appears in both, the explicit map wins.
type LocalRunner struct{}

func (LocalRunner) Run(ctx context.Context, bash string, env map[string]string, allowedPaths []client.AllowedPath) ExecResult {
	start := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, DefaultPerCmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "/bin/bash", "-c", bash)
	if len(allowedPaths) > 0 {
		cmd.Dir = allowedPaths[0].Path
	}
	cmd.Env = mergeEnv(os.Environ(), env)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	dur := time.Since(start)
	res := ExecResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), Duration: dur}

	if runErr == nil {
		return res
	}
	// Timeout: surface DeadlineExceeded so the loop can emit the timeout
	// re-prompt rather than the generic exit-code branch.
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) && !errors.Is(ctx.Err(), context.Canceled) {
		res.ExitCode = -1
		res.Err = context.DeadlineExceeded
		return res
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		res.ExitCode = -1
		res.Err = ctx.Err()
		return res
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res
	}
	res.ExitCode = -1
	res.Err = runErr
	return res
}

// SandboxRunner runs commands in a temenos sandbox. Stdout/stderr are returned
// as separate buffers; ExitCode is the subprocess exit; Err is a runner-level
// failure (daemon unreachable, marshal error).
type SandboxRunner struct {
	Client *client.Client
}

func (s SandboxRunner) Run(ctx context.Context, bash string, env map[string]string, allowedPaths []client.AllowedPath) ExecResult {
	start := time.Now()
	resp, err := s.Client.Run(ctx, client.RunRequest{
		Command:      bash,
		Env:          env,
		AllowedPaths: allowedPaths,
		Timeout:      int(DefaultPerCmdTimeout / time.Second),
	})
	dur := time.Since(start)
	if err != nil {
		return ExecResult{ExitCode: -1, Duration: dur, Err: err}
	}
	return ExecResult{
		Stdout:   []byte(resp.Stdout),
		Stderr:   []byte(resp.Stderr),
		ExitCode: resp.ExitCode,
		Duration: dur,
	}
}

// mergeEnv overlays the explicit env map on top of the parent environment.
// Explicit map keys win on collision so callers can override LENOS_SESSION_ID
// or any inherited variable deterministically.
func mergeEnv(parent []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return parent
	}
	seen := make(map[string]bool, len(overrides))
	merged := make([]string, 0, len(parent)+len(overrides))
	for _, kv := range parent {
		k, _, ok := splitEnvKey(kv)
		if !ok {
			merged = append(merged, kv)
			continue
		}
		if v, has := overrides[k]; has {
			merged = append(merged, k+"="+v)
			seen[k] = true
			continue
		}
		merged = append(merged, kv)
	}
	for k, v := range overrides {
		if seen[k] {
			continue
		}
		merged = append(merged, k+"="+v)
	}
	return merged
}

// splitEnvKey returns (key, value, ok) from an "K=V" pair. Lines without "="
// (rare on Unix) are passed through unchanged by mergeEnv.
func splitEnvKey(kv string) (string, string, bool) {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return "", "", false
}
