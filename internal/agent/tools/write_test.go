package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteTool_ContainedJoin_AcceptsInScopePaths(t *testing.T) {
	t.Parallel()
	workingDir := t.TempDir()

	tool := NewWriteTool(&testHistory{}, &testFiletracker{}, workingDir)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	// Relative path in root.
	resp := runTool(t, tool, ctx, WriteToolName, WriteParams{
		FilePath: "foo.txt",
		Content:  "hello",
	})
	require.False(t, resp.IsError, "relative path should be accepted: %s", resp.Content)
	require.Equal(t, "hello", mustReadFile(t, filepath.Join(workingDir, "foo.txt")))

	// Relative path in subdirectory.
	resp = runTool(t, tool, ctx, WriteToolName, WriteParams{
		FilePath: "sub/dir/bar.txt",
		Content:  "world",
	})
	require.False(t, resp.IsError, "subdirectory path should be accepted: %s", resp.Content)
	require.Equal(t, "world", mustReadFile(t, filepath.Join(workingDir, "sub/dir/bar.txt")))
}

func TestWriteTool_ContainedJoin_RejectsOutOfScopePaths(t *testing.T) {
	t.Parallel()
	workingDir := t.TempDir()

	tool := NewWriteTool(nil, nil, workingDir)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	for _, tc := range escapeCases {
		resp := runTool(t, tool, ctx, WriteToolName, WriteParams{
			FilePath: tc.path,
			Content:  "malicious",
		})
		require.True(t, resp.IsError, "%s should be rejected", tc.description)
		require.Contains(t, resp.Content, "escapes working directory")
	}
}

func TestWriteTool_ContainedJoin_RejectionFiresBeforeFilesystemAccess(t *testing.T) {
	t.Parallel()
	workingDir := t.TempDir()

	// Create a file outside workingDir that the tool should NOT be able to write.
	outsidePath := filepath.Join(t.TempDir(), "should_not_exist.txt")

	tool := NewWriteTool(nil, nil, workingDir)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	resp := runTool(t, tool, ctx, WriteToolName, WriteParams{
		FilePath: "../" + filepath.Base(outsidePath),
		Content:  "should not reach filesystem",
	})
	require.True(t, resp.IsError, "escape attempt should be rejected")

	// Verify the file was NOT created.
	_, err := os.Stat(outsidePath)
	require.True(t, os.IsNotExist(err), "file outside workingDir should not exist after rejected write")
}

func TestWriteTool_ContainedJoin_AcceptsAbsolutePathInsideWorkingDir(t *testing.T) {
	t.Parallel()
	workingDir := t.TempDir()

	tool := NewWriteTool(&testHistory{}, &testFiletracker{}, workingDir)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	absInside := filepath.Join(workingDir, "absolute_inside.txt")
	resp := runTool(t, tool, ctx, WriteToolName, WriteParams{
		FilePath: absInside,
		Content:  "absolute but inside",
	})
	require.False(t, resp.IsError, "absolute path inside workingDir should be accepted: %s", resp.Content)
	require.Equal(t, "absolute but inside", mustReadFile(t, absInside))
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}
