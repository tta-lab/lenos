package hooks

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestShellRunner_EmptyCommand(t *testing.T) {
	r := ShellRunner{Command: ""}
	err := r.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected nil for empty command, got: %v", err)
	}
}

func TestShellRunner_StdinRoundTrip(t *testing.T) {
	payload := []byte(`{"version":1,"event":"post_step"}`)
	var out bytes.Buffer
	r := ShellRunner{
		Command: "cat",
		Stdout:  &out,
	}
	if err := r.Run(context.Background(), payload); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := out.String(); got != string(payload) {
		t.Fatalf("stdout = %q, want %q", got, string(payload))
	}
}

func TestShellRunner_FailingCommand(t *testing.T) {
	r := ShellRunner{Command: "exit 7"}
	err := r.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for exit 7")
	}
	if !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("error = %v, want exit status 7", err)
	}
}

func TestShellRunner_EnvPreserved(t *testing.T) {
	t.Setenv("LENOS_HOOK_TEST", "marker-xyz")
	var out bytes.Buffer
	r := ShellRunner{Command: "printenv LENOS_HOOK_TEST", Stdout: &out}
	if err := r.Run(context.Background(), nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "marker-xyz" {
		t.Fatalf("env not propagated: got %q", got)
	}
}

func TestShellRunner_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	r := ShellRunner{Command: "sleep 10"}
	err := r.Run(ctx, nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") &&
		!strings.Contains(err.Error(), "signal: killed") {
		t.Fatalf("error = %v, want timeout-related error", err)
	}
}

func TestShellRunner_StderrShorterThanCap(t *testing.T) {
	r := ShellRunner{
		Command: "printf '%.0sX' {1..200} >&2; exit 1",
	}
	err := r.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	errMsg := err.Error()
	if strings.Contains(errMsg, "...") {
		t.Fatalf("expected full stderr without truncation for 200B, got: %s", errMsg)
	}
}

func TestShellRunner_StderrAtCap(t *testing.T) {
	r := ShellRunner{
		Command: "printf '%.0sX' {1..256} >&2; exit 1",
	}
	err := r.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	errMsg := err.Error()
	if strings.Contains(errMsg, "...") {
		t.Fatalf("expected exactly 256 bytes without truncation, got: %s", errMsg)
	}
}

func TestShellRunner_StderrLongerThanCap(t *testing.T) {
	r := ShellRunner{
		Command: "printf '%.0sX' {1..300} >&2; exit 1",
	}
	err := r.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "...") {
		t.Fatalf("expected truncated error with ... prefix, got: %s", errMsg)
	}
}
