package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// setupSessionDir chdirs into a tempdir whose .lenos/sessions/ subdir is
// where narrate will write. Returns the .lenos data dir for assertions.
func setupSessionDir(t *testing.T) (dataDir string) {
	t.Helper()
	tmp := t.TempDir()
	dataDir = filepath.Join(tmp, ".lenos")
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "sessions"), 0o755))
	t.Chdir(tmp)
	return dataDir
}

func TestRoot_HappyPath_Args(t *testing.T) {
	dataDir := setupSessionDir(t)

	rootCmd.SetArgs([]string{"hello world"})
	rootCmd.SetIn(strings.NewReader(""))

	t.Setenv("LENOS_SESSION_ID", "test-session")

	require.NoError(t, rootCmd.Execute())

	mdPath := filepath.Join(dataDir, "sessions", "test-session.md")
	data, err := os.ReadFile(mdPath)
	require.NoError(t, err)
	require.Equal(t, "hello world\n\n", string(data))
}

func TestRoot_HappyPath_Stdin(t *testing.T) {
	dataDir := setupSessionDir(t)

	rootCmd.SetArgs([]string{})
	rootCmd.SetIn(strings.NewReader("piped content\n"))

	t.Setenv("LENOS_SESSION_ID", "test-session")

	require.NoError(t, rootCmd.Execute())

	mdPath := filepath.Join(dataDir, "sessions", "test-session.md")
	data, err := os.ReadFile(mdPath)
	require.NoError(t, err)
	require.Equal(t, "piped content\n\n", string(data))
}

func TestRoot_MissingEnv(t *testing.T) {
	rootCmd.SetArgs([]string{"hello"})
	rootCmd.SetIn(strings.NewReader(""))
	t.Setenv("LENOS_SESSION_ID", "")

	err := rootCmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "LENOS_SESSION_ID")
}

func TestRoot_EmptyInput(t *testing.T) {
	setupSessionDir(t)

	rootCmd.SetArgs([]string{})
	rootCmd.SetIn(strings.NewReader(""))

	t.Setenv("LENOS_SESSION_ID", "test-session")

	err := rootCmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "content required")
}

func TestRoot_AppendOnly(t *testing.T) {
	dataDir := setupSessionDir(t)

	t.Setenv("LENOS_SESSION_ID", "test-session")

	rootCmd.SetArgs([]string{"first"})
	rootCmd.SetIn(strings.NewReader(""))
	require.NoError(t, rootCmd.Execute())

	rootCmd.SetArgs([]string{"second"})
	rootCmd.SetIn(strings.NewReader(""))
	require.NoError(t, rootCmd.Execute())

	mdPath := filepath.Join(dataDir, "sessions", "test-session.md")
	data, err := os.ReadFile(mdPath)
	require.NoError(t, err)
	require.Equal(t, "first\n\nsecond\n\n", string(data))
}
