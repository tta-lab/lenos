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

func TestNarrateBinary_EndToEnd(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))
	sessionID := "test-session"
	mdPath := filepath.Join(sessionsDir, sessionID+".md")

	bin := filepath.Join(tmp, "narrate")
	build := exec.Command("go", "build", "-o", bin, "./cmd/narrate")
	build.Dir = repoRoot(t)
	out, err := build.CombinedOutput()
	require.NoError(t, err, "go build: %s", out)

	env := append(os.Environ(),
		"LENOS_SESSION_ID="+sessionID,
		"LENOS_DATA_DIR="+tmp,
	)

	// Invoke `narrate "hello world"` (positional args path).
	args := exec.Command(bin, "hello world")
	args.Env = env
	out, err = args.CombinedOutput()
	require.NoError(t, err, "narrate args: %s", out)

	// Invoke `printf ... | narrate` (piped stdin path).
	pipe := exec.Command(bin)
	pipe.Env = env
	pipe.Stdin = bytes.NewBufferString("first line\nsecond line\n")
	out, err = pipe.CombinedOutput()
	require.NoError(t, err, "narrate pipe: %s", out)

	// Invoke narrate with markdown blockquote via stdin (the visual-emphasis
	// pattern documented in spec 57a09f51 — proves single-mode supports
	// emphasis without severity subcommands).
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
	bin := filepath.Join(tmp, "narrate")
	build := exec.Command("go", "build", "-o", bin, "./cmd/narrate")
	build.Dir = repoRoot(t)
	out, err := build.CombinedOutput()
	require.NoError(t, err, "go build: %s", out)

	cmd := exec.Command(bin, "hello")
	cmd.Env = []string{} // no env at all
	out, err = cmd.CombinedOutput()
	require.Error(t, err, "expected non-zero exit; got nil")
	require.Contains(t, string(out), "LENOS_DATA_DIR", "should mention missing env var")
}
