package hooks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
)

// ShellRunner runs Command via "sh -c". Stdin = the payload bytes. The
// subprocess INHERITS the parent process env (cmd.Env is intentionally left
// nil) — consumers (e.g. "ttal status update") read identity from env vars
// like TTAL_AGENT_NAME. Do not set cmd.Env.
type ShellRunner struct {
	Command string
	Stdout  io.Writer // optional; zero value = io.Discard. Tests set to *bytes.Buffer.
	Stderr  io.Writer // optional; zero value = io.Discard. Tests set to *bytes.Buffer.
}

const stderrTailMax = 256

func (r ShellRunner) Run(ctx context.Context, payload []byte) error {
	if r.Command == "" {
		return nil
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", r.Command)
	cmd.Stdin = bytes.NewReader(payload)

	out := r.Stdout
	if out == nil {
		out = io.Discard
	}
	errw := r.Stderr
	if errw == nil {
		errw = io.Discard
	}

	var stderrBuf bytes.Buffer
	cmd.Stdout = out
	cmd.Stderr = io.MultiWriter(errw, &stderrBuf)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hook %q: %w (stderr: %s)", r.Command, err, tail(stderrBuf.Bytes(), stderrTailMax))
	}
	return nil
}

func tail(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return "..." + string(b[len(b)-n:])
}
