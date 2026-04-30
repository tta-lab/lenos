package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoot_HappyPath_Args(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))

	rootCmd.SetArgs([]string{"hello world"})
	rootCmd.SetIn(strings.NewReader("")) // no stdin

	t.Setenv("LENOS_DATA_DIR", tmp)
	t.Setenv("LENOS_SESSION_ID", "test-session")

	require.NoError(t, rootCmd.Execute())

	mdPath := filepath.Join(sessionsDir, "test-session.md")
	data, err := os.ReadFile(mdPath)
	require.NoError(t, err)
	require.Equal(t, "hello world\n\n", string(data))
}

func TestRoot_HappyPath_Stdin(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))

	rootCmd.SetArgs([]string{})
	rootCmd.SetIn(strings.NewReader("piped content\n"))

	t.Setenv("LENOS_DATA_DIR", tmp)
	t.Setenv("LENOS_SESSION_ID", "test-session")

	require.NoError(t, rootCmd.Execute())

	mdPath := filepath.Join(sessionsDir, "test-session.md")
	data, err := os.ReadFile(mdPath)
	require.NoError(t, err)
	require.Equal(t, "piped content\n\n", string(data))
}

func TestRoot_MissingEnv(t *testing.T) {
	rootCmd.SetArgs([]string{"hello"})
	rootCmd.SetIn(strings.NewReader(""))
	t.Setenv("LENOS_DATA_DIR", "")
	t.Setenv("LENOS_SESSION_ID", "")

	err := rootCmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "LENOS")
}

func TestRoot_EmptyInput(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))

	rootCmd.SetArgs([]string{})
	rootCmd.SetIn(strings.NewReader(""))

	t.Setenv("LENOS_DATA_DIR", tmp)
	t.Setenv("LENOS_SESSION_ID", "test-session")

	err := rootCmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "content required")
}

func TestRoot_AppendOnly(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))

	t.Setenv("LENOS_DATA_DIR", tmp)
	t.Setenv("LENOS_SESSION_ID", "test-session")

	rootCmd.SetArgs([]string{"first"})
	rootCmd.SetIn(strings.NewReader(""))
	require.NoError(t, rootCmd.Execute())

	rootCmd.SetArgs([]string{"second"})
	rootCmd.SetIn(strings.NewReader(""))
	require.NoError(t, rootCmd.Execute())

	mdPath := filepath.Join(sessionsDir, "test-session.md")
	data, err := os.ReadFile(mdPath)
	require.NoError(t, err)
	require.Equal(t, "first\n\nsecond\n\n", string(data))
}
