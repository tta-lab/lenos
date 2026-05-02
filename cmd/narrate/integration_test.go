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

// setupSessionRoot returns (rootDir, dataDir). narrate run with cwd=rootDir
// will resolve dataDir via LookupClosest.
func setupSessionRoot(t *testing.T) (root, dataDir string) {
	t.Helper()
	root = t.TempDir()
	dataDir = filepath.Join(root, ".lenos")
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "sessions"), 0o755))
	return root, dataDir
}

func TestNarrateBinary_EndToEnd(t *testing.T) {
	root, dataDir := setupSessionRoot(t)
	sessionID := "test-session"
	mdPath := filepath.Join(dataDir, "sessions", sessionID+".md")

	bin := buildNarrateBinary(t, t.TempDir())
	env := append(os.Environ(), "LENOS_SESSION_ID="+sessionID)

	args := exec.Command(bin, "hello world")
	args.Env = env
	args.Dir = root
	out, err := args.CombinedOutput()
	require.NoError(t, err, "narrate args: %s", out)

	pipe := exec.Command(bin)
	pipe.Env = env
	pipe.Dir = root
	pipe.Stdin = bytes.NewBufferString("first line\nsecond line\n")
	out, err = pipe.CombinedOutput()
	require.NoError(t, err, "narrate pipe: %s", out)

	bq := exec.Command(bin)
	bq.Env = env
	bq.Dir = root
	bq.Stdin = bytes.NewBufferString("> ⚠️ markdown blockquote\n")
	out, err = bq.CombinedOutput()
	require.NoError(t, err, "narrate blockquote: %s", out)

	got, err := os.ReadFile(mdPath)
	require.NoError(t, err)
	expected := "hello world\n\n" +
		"first line\nsecond line\n\n" +
		"> ⚠️ markdown blockquote\n\n"
	require.Equal(t, expected, string(got))
}

func TestNarrateBinary_FailsWithoutSessionID(t *testing.T) {
	tmp := t.TempDir()
	bin := buildNarrateBinary(t, tmp)

	cmd := exec.Command(bin, "hello")
	cmd.Env = []string{} // no LENOS_SESSION_ID
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	assertStderrPrefix(t, out, err, "LENOS_SESSION_ID")
}

func TestNarrateBinary_FailsOnEmptyStdin(t *testing.T) {
	root, _ := setupSessionRoot(t)
	bin := buildNarrateBinary(t, t.TempDir())

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "LENOS_SESSION_ID=test-session")
	cmd.Dir = root
	cmd.Stdin = bytes.NewBufferString("")
	out, err := cmd.CombinedOutput()
	assertStderrPrefix(t, out, err, "content required")
}
