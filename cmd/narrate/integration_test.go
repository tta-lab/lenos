//go:build integration

package main_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	// integration_test.go lives at cmd/narrate/; repo root is two levels up.
	return filepath.Dir(filepath.Dir(wd))
}

// buildNarrateBinary compiles the narrate binary into tmp and returns its path.
func buildNarrateBinary(t *testing.T, tmp string) string {
	bin := filepath.Join(tmp, "narrate")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/narrate")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go build: %s", out)
	return bin
}

// assertStderrPrefix checks that cmd exited non-zero and its stderr begins with
// the "narrate:" prefix, confirming E14 fail-loud message formatting.
func assertStderrPrefix(t *testing.T, out []byte, err error, contains string) {
	t.Helper()
	require.Error(t, err, "expected non-zero exit")
	require.Contains(t, string(out), "narrate:", "stderr should be prefixed with 'narrate:'")
	require.Contains(t, string(out), contains)
}

func TestNarrateBinary_EndToEnd(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))
	sessionID := "test-session"
	mdPath := filepath.Join(sessionsDir, sessionID+".md")

	bin := buildNarrateBinary(t, tmp)

	env := append(os.Environ(),
		"LENOS_SESSION_ID="+sessionID,
		"LENOS_DATA_DIR="+tmp,
	)

	// Invoke `narrate "hello world"` (positional args path).
	args := exec.Command(bin, "hello world")
	args.Env = env
	out, err := args.CombinedOutput()
	require.NoError(t, err, "narrate args: %s", out)

	// Invoke `printf ... | narrate` (piped stdin path).
	pipe := exec.Command(bin)
	pipe.Env = env
	pipe.Stdin = bytes.NewBufferString("first line\nsecond line\n")
	out, err = pipe.CombinedOutput()
	require.NoError(t, err, "narrate pipe: %s", out)

	// Invoke narrate with a markdown blockquote via stdin — confirms single-mode
	// doesn't corrupt markdown formatting from stdin.
	bq := exec.Command(bin)
	bq.Env = env
	bq.Stdin = bytes.NewBufferString("> ⚠️ markdown blockquote\n")
	out, err = bq.CombinedOutput()
	require.NoError(t, err, "narrate blockquote: %s", out)

	// Read the .md file and assert contents.
	got, err := os.ReadFile(mdPath)
	require.NoError(t, err)
	expected := "hello world\n\n" +
		"first line\nsecond line\n\n" +
		"> ⚠️ markdown blockquote\n\n"
	require.Equal(t, expected, string(got))
}

func TestNarrateBinary_FailsWithoutEnv(t *testing.T) {
	tmp := t.TempDir()
	bin := buildNarrateBinary(t, tmp)

	cmd := exec.Command(bin, "hello")
	cmd.Env = []string{} // no env at all
	out, err := cmd.CombinedOutput()
	assertStderrPrefix(t, out, err, "LENOS_DATA_DIR")
}

func TestNarrateBinary_FailsOnEmptyStdin(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))
	sessionID := "test-session"

	bin := buildNarrateBinary(t, tmp)

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"LENOS_SESSION_ID="+sessionID,
		"LENOS_DATA_DIR="+tmp,
	)
	cmd.Stdin = bytes.NewBufferString("") // empty pipe
	out, err := cmd.CombinedOutput()
	assertStderrPrefix(t, out, err, "content required")
}
