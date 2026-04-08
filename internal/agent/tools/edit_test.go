package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

func runEditTool(t *testing.T, tool fantasy.AgentTool, ctx context.Context, params EditParams) fantasy.ToolResponse {
	t.Helper()
	input, err := json.Marshal(params)
	require.NoError(t, err)
	call := fantasy.ToolCall{
		ID:    "test-edit-call",
		Name:  EditToolName,
		Input: string(input),
	}
	resp, err := tool.Run(ctx, call)
	require.NoError(t, err)
	return resp
}

func TestEditTool_ContainedJoin_RejectsOutOfScopePaths(t *testing.T) {
	t.Parallel()
	workingDir := t.TempDir()

	tool := NewEditTool(nil, nil, nil, workingDir)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	// Absolute path outside working directory.
	resp := runEditTool(t, tool, ctx, EditParams{
		FilePath:  "/etc/passwd",
		OldString: "root",
		NewString: "hacker",
	})
	require.True(t, resp.IsError, "absolute path outside CWD should be rejected")
	require.Contains(t, resp.Content, "escapes working directory")

	// Relative path escaping via parent directory.
	resp = runEditTool(t, tool, ctx, EditParams{
		FilePath:  "../other/foo.txt",
		OldString: "old",
		NewString: "new",
	})
	require.True(t, resp.IsError, "path with .. should be rejected")
	require.Contains(t, resp.Content, "escapes working directory")

	// Deeply nested escape.
	resp = runEditTool(t, tool, ctx, EditParams{
		FilePath:  "sub/../../etc/passwd",
		OldString: "old",
		NewString: "new",
	})
	require.True(t, resp.IsError, "deeply nested escape should be rejected")
	require.Contains(t, resp.Content, "escapes working directory")
}

func TestEditTool_ContainedJoin_RejectionFiresBeforeFilesystemAccess(t *testing.T) {
	t.Parallel()
	workingDir := t.TempDir()

	// Create a file outside workingDir that the tool should NOT be able to access.
	outsidePath := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outsidePath, []byte("secret content"), 0o600))

	tool := NewEditTool(nil, nil, nil, workingDir)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	// Attempt to escape to a sibling directory.
	resp := runEditTool(t, tool, ctx, EditParams{
		FilePath:  "../" + filepath.Base(outsidePath),
		OldString: "secret",
		NewString: "XXX",
	})
	require.True(t, resp.IsError, "escape attempt should be rejected")
	require.Contains(t, resp.Content, "escapes working directory")

	// Verify the file was NOT modified.
	content, err := os.ReadFile(outsidePath)
	require.NoError(t, err)
	require.Equal(t, "secret content", string(content), "file outside workingDir should not have been modified")
}
