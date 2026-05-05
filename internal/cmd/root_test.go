package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootCmd_ReadonlyFlagDeclared(t *testing.T) {
	f := rootCmd.Flags().Lookup("readonly")
	require.NotNil(t, f, "--readonly flag must be declared on rootCmd")
	require.Equal(t, "", f.Shorthand, "--readonly should have no shorthand")
	require.Equal(t, "false", f.DefValue, "--readonly default must be false")
}

func TestRootCmd_ReadonlyFlagParse(t *testing.T) {
	err := rootCmd.ParseFlags([]string{"--readonly"})
	require.NoError(t, err)
	v, _ := rootCmd.Flags().GetBool("readonly")
	require.True(t, v)
}

func TestResolveAgentFile_FoundOnDisk(t *testing.T) {
	td := t.TempDir()
	agentContent := "# Test Agent\nBody"
	if err := os.WriteFile(filepath.Join(td, "testagent.md"), []byte(agentContent), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := resolveAgentFile("testagent", []string{td})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got: %s", path)
	}
}

func TestResolveAgentFile_CoderFallsBackToEmbedded(t *testing.T) {
	td := t.TempDir()
	path, err := resolveAgentFile("coder", []string{td})
	if err != nil {
		t.Fatalf("expected no error for coder fallback, got: %v", err)
	}
	if path != "" {
		t.Errorf("expected empty path for embedded fallback, got: %s", path)
	}
}

func TestResolveAgentFile_NonCoderNotFound_Errors(t *testing.T) {
	td := t.TempDir()
	_, err := resolveAgentFile("nonexistent", []string{td})
	if err == nil {
		t.Fatal("expected error for non-coder agent not found")
	}
}
