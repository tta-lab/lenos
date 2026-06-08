package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tta-lab/lenos/internal/config"

	"github.com/stretchr/testify/require"
)

func TestRootCmd_ReadonlyFlagDeclared(t *testing.T) {
	f := rootCmd.Flags().Lookup("readonly")
	require.NotNil(t, f, "--readonly flag must be declared on rootCmd")
	require.Equal(t, "", f.Shorthand, "--readonly should have no shorthand")
	require.Equal(t, "false", f.DefValue, "--readonly default must be false")
}

func TestRootCmd_NoSandboxFlagDeclared(t *testing.T) {
	f := rootCmd.Flags().Lookup("no-sandbox")
	require.NotNil(t, f, "--no-sandbox flag must be declared on rootCmd")
	require.Equal(t, "", f.Shorthand, "--no-sandbox should have no shorthand")
	require.Equal(t, "false", f.DefValue, "--no-sandbox default must be false")
}

func TestRootCmd_ReadonlyFlagParse(t *testing.T) {
	err := rootCmd.ParseFlags([]string{"--readonly"})
	require.NoError(t, err)
	v, _ := rootCmd.Flags().GetBool("readonly")
	require.True(t, v)
}

func TestRootCmd_NoSandboxFlagParse(t *testing.T) {
	err := rootCmd.ParseFlags([]string{"--no-sandbox"})
	require.NoError(t, err)
	v, _ := rootCmd.Flags().GetBool("no-sandbox")
	require.True(t, v)
}

func TestReadonlySandboxPolicy_ConfigSandboxFalseFails(t *testing.T) {
	disabled := false
	err := validateReadonlySandboxPolicy(true, &disabled, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "--readonly requires sandbox enforcement")
	require.Contains(t, err.Error(), "options.sandbox=false")
}

func TestReadonlySandboxPolicy_NoSandboxFlagFails(t *testing.T) {
	err := validateReadonlySandboxPolicy(true, nil, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "--readonly requires sandbox enforcement")
	require.Contains(t, err.Error(), "--no-sandbox")
}

func TestRootCmd_PairWithFlagDeclared(t *testing.T) {
	f := rootCmd.Flags().Lookup("pair-with")
	require.NotNil(t, f, "--pair-with flag must be declared on rootCmd")
	require.Equal(t, "", f.Shorthand, "--pair-with should have no shorthand")
	require.Equal(t, "", f.DefValue, "--pair-with default must be empty")
}

func TestRootCmd_ReasoningEffortFlagDeclared(t *testing.T) {
	f := rootCmd.Flags().Lookup("reasoning-effort")
	require.NotNil(t, f, "--reasoning-effort flag must be declared on rootCmd")
	require.Equal(t, "", f.Shorthand, "--reasoning-effort should have no shorthand")
	require.Equal(t, "", f.DefValue, "--reasoning-effort default must be empty")
}

func TestRootCmd_PairWithFlagParse(t *testing.T) {
	err := rootCmd.ParseFlags([]string{"--pair-with", "reviewer"})
	require.NoError(t, err)
	v, _ := rootCmd.Flags().GetString("pair-with")
	require.Equal(t, "reviewer", v)
}

func TestFormatResumeHint(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		"To continue this session, run lenos --session session-123",
		formatResumeHint("session-123"),
	)
	require.Empty(t, formatResumeHint(""))
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

func TestResolveWorkspaceAgent_DefaultUsesEmbeddedCoderEvenWhenCoderFileExists(t *testing.T) {
	td := t.TempDir()
	if err := os.WriteFile(filepath.Join(td, "coder.md"), []byte("# Claude Coder"), 0o644); err != nil {
		t.Fatal(err)
	}

	agentName, path, err := resolveWorkspaceAgent("", []string{td})
	if err != nil {
		t.Fatalf("expected no error for default coder fallback, got: %v", err)
	}
	if agentName != config.AgentCoder {
		t.Errorf("expected default agent %q, got %q", config.AgentCoder, agentName)
	}
	if path != "" {
		t.Errorf("expected empty path for embedded default coder, got: %s", path)
	}
}

func TestResolveAgentFile_ReviewerFallsBackToEmbedded(t *testing.T) {
	td := t.TempDir()
	path, err := resolveAgentFile("reviewer", []string{td})
	if err != nil {
		t.Fatalf("expected no error for reviewer fallback, got: %v", err)
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

func TestResolveAgentFile_FoundFolderFormat(t *testing.T) {
	td := t.TempDir()
	agentDir := filepath.Join(td, "yuki")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentContent := "# Yuki\nManager body"
	if err := os.WriteFile(filepath.Join(agentDir, "AGENTS.md"), []byte(agentContent), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := resolveAgentFile("yuki", []string{td})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got: %s", path)
	}
	if filepath.Base(path) != "AGENTS.md" {
		t.Errorf("expected resolved file to be AGENTS.md, got: %s", path)
	}
}

func TestResolveAgentFile_FlatTakesPrecedenceOverFolder(t *testing.T) {
	td := t.TempDir()
	// Flat: <td>/yuki.md
	if err := os.WriteFile(filepath.Join(td, "yuki.md"), []byte("# Flat"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Folder: <td>/yuki/AGENTS.md
	agentDir := filepath.Join(td, "yuki")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "AGENTS.md"), []byte("# Folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := resolveAgentFile("yuki", []string{td})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	// Flat should win.
	if filepath.Base(path) != "yuki.md" {
		t.Errorf("expected flat yuki.md to win, got: %s", path)
	}
}

func TestResolveAgentFile_MixedDirs(t *testing.T) {
	flatDir := t.TempDir()
	folderDir := t.TempDir()
	// Flat agent in flatDir
	if err := os.WriteFile(filepath.Join(flatDir, "ask.md"), []byte("# Ask"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Folder agent in folderDir
	yukiDir := filepath.Join(folderDir, "yuki")
	if err := os.MkdirAll(yukiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(yukiDir, "AGENTS.md"), []byte("# Yuki"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := []string{flatDir, folderDir}
	// Both names resolve.
	askPath, err := resolveAgentFile("ask", paths)
	if err != nil {
		t.Fatalf("ask: expected no error, got: %v", err)
	}
	if filepath.Base(askPath) != "ask.md" {
		t.Errorf("ask: expected ask.md, got: %s", askPath)
	}
	yukiPath, err := resolveAgentFile("yuki", paths)
	if err != nil {
		t.Fatalf("yuki: expected no error, got: %v", err)
	}
	if filepath.Base(yukiPath) != "AGENTS.md" {
		t.Errorf("yuki: expected AGENTS.md, got: %s", yukiPath)
	}
}

func TestResolveAgentFile_PathPrecedenceAcrossShapes(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	// dir1 has folder-format yuki
	yukiDir1 := filepath.Join(dir1, "yuki")
	if err := os.MkdirAll(yukiDir1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(yukiDir1, "AGENTS.md"), []byte("# Folder dir1"), 0o644); err != nil {
		t.Fatal(err)
	}
	// dir2 has flat-format yuki
	if err := os.WriteFile(filepath.Join(dir2, "yuki.md"), []byte("# Flat dir2"), 0o644); err != nil {
		t.Fatal(err)
	}
	// dir1 listed first → folder match in dir1 should win over flat match in dir2.
	path, err := resolveAgentFile("yuki", []string{dir1, dir2})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(path, dir1) {
		t.Errorf("expected dir1 to win, got: %s", path)
	}
	if filepath.Base(path) != "AGENTS.md" {
		t.Errorf("expected dir1's AGENTS.md, got: %s", path)
	}
}

func TestResolveAgentFile_EmptyStringDirSkipped(t *testing.T) {
	td := t.TempDir()
	agentContent := "# Test"
	if err := os.WriteFile(filepath.Join(td, "testagent.md"), []byte(agentContent), 0o644); err != nil {
		t.Fatal(err)
	}
	// Empty string first; should be skipped, real dir wins.
	path, err := resolveAgentFile("testagent", []string{"", td})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path from second dir")
	}
	if !strings.Contains(path, td) {
		t.Errorf("expected resolution from real dir, got: %s", path)
	}
}

func TestResolveAgentFile_CoderFoundInFolderFormat(t *testing.T) {
	td := t.TempDir()
	coderDir := filepath.Join(td, "coder")
	if err := os.MkdirAll(coderDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(coderDir, "AGENTS.md"), []byte("# Custom Coder"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := resolveAgentFile("coder", []string{td})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	// On-disk coder MUST take precedence over embedded fallback.
	if path == "" {
		t.Fatal("expected non-empty path; on-disk coder/AGENTS.md must beat embedded fallback")
	}
	if filepath.Base(path) != "AGENTS.md" {
		t.Errorf("expected AGENTS.md, got: %s", path)
	}
}

func TestCreateDotLenosDir_CreatesRuntimeDirs(t *testing.T) {
	td := t.TempDir()
	dataDir := filepath.Join(td, ".lenos")

	err := createDotLenosDir(dataDir)
	require.NoError(t, err)

	require.DirExists(t, dataDir)
	require.DirExists(t, filepath.Join(dataDir, "sessions"))
	require.DirExists(t, filepath.Join(dataDir, "logs"))

	gitignorePath := filepath.Join(dataDir, ".gitignore")
	require.FileExists(t, gitignorePath)
	content, readErr := os.ReadFile(gitignorePath)
	require.NoError(t, readErr)
	require.Equal(t, defaultGitIgnore, string(content))
}
