package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/tta-lab/temenos/sandbox"
)

// DefaultPerCmdTimeout caps a single bash subprocess. Matches the pre-existing
// temenos sandbox default. Agents can override via bash-native `timeout 30m`.
const DefaultPerCmdTimeout = 120 * time.Second

// defaultAutoBackgroundAfter is the threshold before a sandbox command is
// detached into a background job. Commands completing faster return
// synchronously.
var defaultAutoBackgroundAfter = 15 * time.Second

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
type LocalRunner struct {
	bg *BackgroundRunner
}

func (l LocalRunner) Run(ctx context.Context, bash string, env map[string]string, allowedPaths []AllowedPath) ExecResult {
	start := time.Now()
	runCtx, cancel := context.WithTimeout(context.Background(), DefaultPerCmdTimeout)

	done := make(chan execOut, 1)
	go func() {
		cmd := exec.CommandContext(runCtx, "/bin/bash", "-c", bash)
		if len(allowedPaths) > 0 {
			cmd.Dir = allowedPaths[0].Path
		}
		cmd.Env = mergeEnv(os.Environ(), env)

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		runErr := cmd.Run()
		out := execOut{
			stdout:   stdout.String(),
			stderr:   stderr.String(),
			exitCode: 0,
		}
		if runErr == nil {
			done <- out
			return
		}
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			out.exitCode = -1
			out.err = context.DeadlineExceeded
			done <- out
			return
		}
		if errors.Is(runCtx.Err(), context.Canceled) {
			out.exitCode = -1
			out.err = context.Canceled
			done <- out
			return
		}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			out.exitCode = exitErr.ExitCode()
			done <- out
			return
		}
		out.exitCode = -1
		out.err = runErr
		done <- out
	}()

	return waitForegroundOrBackground(ctx, start, bash, l.bg, cancel, done)
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

	env = applyNonInteractiveDefaults(env)
	envSlice := make([]string, 0, len(env))
	for k, v := range env {
		envSlice = append(envSlice, k+"="+v)
	}

	execCfg := &sandbox.ExecConfig{
		Env:        envSlice,
		MountDirs:  mounts,
		WorkingDir: workDir,
	}

	bgCtx, cancel := context.WithCancel(context.Background())

	done := make(chan execOut, 1)
	go func() {
		stdout, stderr, exitCode, err := s.sbx.Exec(bgCtx, bash, execCfg)
		done <- execOut{stdout, stderr, exitCode, err}
	}()

	return waitForegroundOrBackground(ctx, start, bash, s.bg, cancel, done)
}

type execOut struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
}

func waitForegroundOrBackground(
	ctx context.Context,
	start time.Time,
	bash string,
	bg *BackgroundRunner,
	cancel context.CancelFunc,
	done <-chan execOut,
) ExecResult {
	autoBgTimer := time.NewTimer(defaultAutoBackgroundAfter)
	defer autoBgTimer.Stop()

	select {
	case out := <-done:
		cancel()
		return execResultFromOut(out, time.Since(start))
	case <-ctx.Done():
		cancel()
		return ExecResult{ExitCode: -1, Duration: time.Since(start), Err: ctx.Err()}
	case <-autoBgTimer.C:
		if bg == nil {
			select {
			case out := <-done:
				cancel()
				return execResultFromOut(out, time.Since(start))
			case <-ctx.Done():
				cancel()
				return ExecResult{ExitCode: -1, Duration: time.Since(start), Err: ctx.Err()}
			}
		}

		jobID := newJobID()
		killCh := make(chan struct{})
		var killOnce sync.Once
		kill := func() {
			killOnce.Do(func() {
				close(killCh)
			})
		}
		resultCh := make(chan backgroundResult, 1)
		go func() {
			resultCh <- waitBackgroundResult(done, cancel, killCh)
			close(resultCh)
		}()
		bg.Track(jobID, bash, kill, resultCh)
		return ExecResult{
			JobID:      jobID,
			Background: true,
			Duration:   time.Since(start),
		}
	}
}

func waitBackgroundResult(done <-chan execOut, cancel context.CancelFunc, killCh <-chan struct{}) backgroundResult {
	select {
	case out := <-done:
		cancel()
		return backgroundResultFromOut(out, false)
	default:
	}

	select {
	case out := <-done:
		cancel()
		return backgroundResultFromOut(out, false)
	case <-killCh:
		select {
		case out := <-done:
			cancel()
			return backgroundResultFromOut(out, false)
		default:
		}
		cancel()
		out := <-done
		return backgroundResultFromOut(out, true)
	}
}

func backgroundResultFromOut(out execOut, killed bool) backgroundResult {
	return backgroundResult{
		stdout:   out.stdout,
		stderr:   out.stderr,
		exitCode: out.exitCode,
		err:      out.err,
		killed:   killed,
	}
}

func execResultFromOut(out execOut, dur time.Duration) ExecResult {
	return ExecResult{
		Stdout:   []byte(out.stdout),
		Stderr:   []byte(out.stderr),
		ExitCode: out.exitCode,
		Duration: dur,
		Err:      out.err,
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
