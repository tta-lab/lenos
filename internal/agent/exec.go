package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tta-lab/temenos/sandbox"
)

// DefaultPerCmdTimeout caps a single bash subprocess. Matches the pre-existing
// temenos sandbox default. Agents can override via bash-native `timeout 30m`.
const DefaultPerCmdTimeout = 120 * time.Second

// defaultAutoBackgroundAfter is the threshold in seconds before a sandbox
// command is detached into a background job. Commands completing faster
// return synchronously.
const defaultAutoBackgroundAfter = 15

// ExecResult is the outcome of running one agent emit through a Runner.
//
//   - ExitCode is the subprocess exit code on normal exit, -1 on runner-level
//     failure (timeout, missing binary, sandbox daemon error).
//   - Err is non-nil only on runner-level failures, NOT on non-zero exit. A
//     timeout sets Err to context.DeadlineExceeded so the loop can branch on it.
type ExecResult struct {
	Stdout     []byte
	Stderr     []byte
	ExitCode   int
	Duration   time.Duration
	Err        error
	JobID      string
	Background bool
}

// Runner abstracts the execution backend (local subprocess or temenos sandbox).
// The single method keeps the interface trivial; tests can pass a fake.
type Runner interface {
	Run(ctx context.Context, bash string, env map[string]string, allowedPaths []AllowedPath) ExecResult
}

// LocalRunner runs commands via /bin/bash -c on the host.
//
// Env handling: the subprocess inherits the parent process environment
// (os.Environ()) and overlays the explicit env map on top. This preserves
// PATH/HOME/TMPDIR while letting the loop set LENOS_* and other per-call
// values. If a key appears in both, the explicit map wins.
//
// Path restriction: allowedPaths[0].Path is used to set the subprocess working
// directory (cmd.Dir) for relative-path ergonomics. LocalRunner does NOT
// enforce read/write boundaries — the subprocess can access any path via
// absolute paths or cd regardless of AllowedPaths. Use SandboxRunner for
// actual sandboxing.
type LocalRunner struct{}

func (LocalRunner) Run(ctx context.Context, bash string, env map[string]string, allowedPaths []AllowedPath) ExecResult {
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

// SandboxedRunner runs commands via the temenos sandbox SDK directly — no
// daemon or socket. On first use it loads ~/.config/temenos/config.toml
// and builds a reusable Sandbox. On darwin, Exec generates seatbelt profiles
// and calls sandbox-exec. On linux, it uses bubblewrap namespace isolation.
type SandboxedRunner struct {
	sbx sandbox.Sandbox
	cfg *sandbox.Config
	bg  *BackgroundRunner
}

func (s *SandboxedRunner) Run(ctx context.Context, bash string, env map[string]string, allowedPaths []AllowedPath) ExecResult {
	start := time.Now()
	if s.sbx == nil {
		var err error
		s.cfg, s.sbx, err = sandbox.LoadConfig("")
		if err != nil {
			return ExecResult{ExitCode: -1, Duration: time.Since(start), Err: fmt.Errorf("load temenos config: %w", err)}
		}
	}

	mounts := s.cfg.BaselineMounts()
	for _, p := range allowedPaths {
		mounts = append(mounts, sandbox.Mount{
			Source:   p.Path,
			Target:   p.Path,
			ReadOnly: p.ReadOnly,
		})
	}

	workDir := "/tmp"
	if len(allowedPaths) > 0 {
		workDir = allowedPaths[0].Path
	}

	envSlice := make([]string, 0, len(env))
	for k, v := range env {
		envSlice = append(envSlice, k+"="+v)
	}

	execCfg := &sandbox.ExecConfig{
		Env:        envSlice,
		MountDirs:  mounts,
		WorkingDir: workDir,
	}

	// Spawn execution in a goroutine; wait up to the auto-background threshold.
	// If it completes in time, return synchronously. Otherwise, hand off to
	// BackgroundRunner and return Background: true.
	type execOut struct {
		stdout, stderr string
		exitCode       int
		err            error
	}
	done := make(chan execOut, 1)
	bgCtx, bgCancel := context.WithCancel(context.Background())
	go func() {
		stdout, stderr, exitCode, err := s.sbx.Exec(bgCtx, bash, execCfg)
		done <- execOut{stdout, stderr, exitCode, err}
	}()

	autoBgTimer := time.NewTimer(time.Duration(defaultAutoBackgroundAfter) * time.Second)
	select {
	case out := <-done:
		autoBgTimer.Stop()
		bgCancel()
		dur := time.Since(start)
		if out.err != nil {
			return ExecResult{Stdout: []byte(out.stdout), Stderr: []byte(out.stderr), ExitCode: out.exitCode, Duration: dur, Err: out.err}
		}
		return ExecResult{
			Stdout:   []byte(out.stdout),
			Stderr:   []byte(out.stderr),
			ExitCode: out.exitCode,
			Duration: dur,
		}
	case <-autoBgTimer.C:
		// Command still running — hand to BackgroundRunner if available.
		if s.bg == nil {
			// No runner; wait for completion.
			out := <-done
			bgCancel()
			dur := time.Since(start)
			if out.err != nil {
				return ExecResult{Stdout: []byte(out.stdout), Stderr: []byte(out.stderr), ExitCode: out.exitCode, Duration: dur, Err: out.err}
			}
			return ExecResult{
				Stdout:   []byte(out.stdout),
				Stderr:   []byte(out.stderr),
				ExitCode: out.exitCode,
				Duration: dur,
			}
		}

		jobID := newJobID()
		resultCh := make(chan backgroundResult, 1)
		go func() {
			out := <-done
			bgCancel()
			resultCh <- backgroundResult{
				stdout:   out.stdout,
				stderr:   out.stderr,
				exitCode: out.exitCode,
				err:      out.err,
			}
			close(resultCh)
		}()
		s.bg.Track(jobID, bash, bgCancel, resultCh)
		return ExecResult{
			JobID:      jobID,
			Background: true,
			Duration:   time.Since(start),
		}
	}
}

// mergeEnv overlays the explicit env map on top of the parent environment.
// Explicit map keys win on collision so callers can override LENOS_SESSION_ID
// or any inherited variable deterministically.
// applyNonInteractiveDefaults ensures the env map has safe defaults that
// prevent commands from spawning editors, pagers, or other interactive TUI
// programs that would hang on a nonexistent terminal. Caller-supplied values
// take precedence (already-set keys are not overwritten). Ported from
// upstream fix c2be8cbf.
func applyNonInteractiveDefaults(env map[string]string) map[string]string {
	const nonInteractiveEnv = "TERM=xterm-256color\x00EDITOR=false\x00VISUAL=false\x00PAGER=cat\x00GIT_PAGER=cat\x00JJ_EDITOR=false\x00JJ_PAGER=cat"
	for _, kv := range strings.Split(nonInteractiveEnv, "\x00") {
		k, v, _ := strings.Cut(kv, "=")
		if _, has := env[k]; !has {
			if env == nil {
				env = make(map[string]string)
			}
			env[k] = v
		}
	}
	return env
}

func mergeEnv(parent []string, overrides map[string]string) []string {
	overrides = applyNonInteractiveDefaults(overrides)
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
