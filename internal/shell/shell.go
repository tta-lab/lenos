// Package shell provides cross-platform shell execution capabilities.
//
// This package provides Shell instances for executing commands with their own
// working directory and environment. Each shell execution is independent.
//
// WINDOWS COMPATIBILITY:
// This implementation provides POSIX shell emulation (mvdan.cc/sh/v3) even on
// Windows. Commands should use forward slashes (/) as path separators to work
// correctly on all platforms.
package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"mvdan.cc/sh/moreinterp/coreutils"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// ShellType represents the type of shell to use
type ShellType int

const (
	ShellTypePOSIX ShellType = iota
	ShellTypeCmd
	ShellTypePowerShell
)

// Logger interface for optional logging
type Logger interface {
	InfoPersist(msg string, keysAndValues ...any)
}

// noopLogger is a logger that does nothing
type noopLogger struct{}

func (noopLogger) InfoPersist(msg string, keysAndValues ...any) {}

// Shell provides cross-platform shell execution with optional state persistence
type Shell struct {
	env    []string
	cwd    string
	mu     sync.Mutex
	logger Logger
}

// Options for creating a new shell
type Options struct {
	WorkingDir string
	Env        []string
	Logger     Logger
}

// NewShell creates a new shell instance with the given options
func NewShell(opts *Options) *Shell {
	if opts == nil {
		opts = &Options{}
	}

	cwd := opts.WorkingDir
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	env := opts.Env
	if env == nil {
		env = os.Environ()
	}

	// Allow tools to detect execution by Lenos.
	env = append(
		env,
		"CRUSH=1",
		"AGENT=lenos",
		"AI_AGENT=lenos",
	)

	logger := opts.Logger
	if logger == nil {
		logger = noopLogger{}
	}

	return &Shell{
		cwd:    cwd,
		env:    env,
		logger: logger,
	}
}

// Exec executes a command in the shell
func (s *Shell) Exec(ctx context.Context, command string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.exec(ctx, command)
}

// ExecStream executes a command in the shell with streaming output to provided writers
func (s *Shell) ExecStream(ctx context.Context, command string, stdout, stderr io.Writer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.execStream(ctx, command, stdout, stderr)
}

// GetWorkingDir returns the current working directory
func (s *Shell) GetWorkingDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cwd
}

// SetWorkingDir sets the working directory
func (s *Shell) SetWorkingDir(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify the directory exists
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("directory does not exist: %w", err)
	}

	s.cwd = dir
	return nil
}

// GetEnv returns a copy of the environment variables
func (s *Shell) GetEnv() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	env := make([]string, len(s.env))
	copy(env, s.env)
	return env
}

// SetEnv sets an environment variable
func (s *Shell) SetEnv(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update or add the environment variable
	keyPrefix := key + "="
	for i, env := range s.env {
		if strings.HasPrefix(env, keyPrefix) {
			s.env[i] = keyPrefix + value
			return
		}
	}
	s.env = append(s.env, keyPrefix+value)
}

// newInterp creates a new interpreter with the current shell state
func (s *Shell) newInterp(stdout, stderr io.Writer) (*interp.Runner, error) {
	return interp.New(
		interp.StdIO(nil, stdout, stderr),
		interp.Interactive(false),
		interp.Env(expand.ListEnviron(s.env...)),
		interp.Dir(s.cwd),
		interp.ExecHandlers(s.execHandlers()...),
	)
}

// updateShellFromRunner updates the shell from the interpreter after execution.
func (s *Shell) updateShellFromRunner(runner *interp.Runner) {
	s.cwd = runner.Dir
	s.env = s.env[:0]
	for name, vr := range runner.Vars {
		if vr.Exported {
			s.env = append(s.env, name+"="+vr.Str)
		}
	}
}

// execCommon is the shared implementation for executing commands
func (s *Shell) execCommon(ctx context.Context, command string, stdout, stderr io.Writer) (err error) {
	var runner *interp.Runner
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("command execution panic: %v", r)
		}
		if runner != nil {
			s.updateShellFromRunner(runner)
		}
		s.logger.InfoPersist("command finished", "command", command, "err", err)
	}()

	line, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return fmt.Errorf("could not parse command: %w", err)
	}

	runner, err = s.newInterp(stdout, stderr)
	if err != nil {
		return fmt.Errorf("could not run command: %w", err)
	}

	err = runner.Run(ctx, line)
	return err
}

// exec executes commands using a cross-platform shell interpreter.
func (s *Shell) exec(ctx context.Context, command string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	err := s.execCommon(ctx, command, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

// execStream executes commands using POSIX shell emulation with streaming output
func (s *Shell) execStream(ctx context.Context, command string, stdout, stderr io.Writer) error {
	return s.execCommon(ctx, command, stdout, stderr)
}

func (s *Shell) execHandlers() []func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	handlers := []func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc{}
	if useGoCoreUtils {
		handlers = append(handlers, coreutils.ExecHandler)
	}
	return handlers
}

// IsInterrupt checks if an error is due to interruption
func IsInterrupt(err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

// ExitCode extracts the exit code from an error
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr interp.ExitStatus
	if errors.As(err, &exitErr) {
		return int(exitErr)
	}
	return 1
}
