package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEditTool_ContainedJoin_AcceptsInScopePaths(t *testing.T) {
	t.Parallel()
	workingDir := workingDirForTest(t.TempDir())
	ft := &testFiletracker{}

	// Pre-create a file to edit.
	testFile := filepath.Join(workingDir, "existing.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("hello world"), 0o644))
	ft.MarkAsRead("test-session", testFile)

	tool := NewEditTool(nil, &testHistory{}, ft, workingDir)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	// Relative path to existing file.
	resp := runTool(t, tool, ctx, EditToolName, EditParams{
		FilePath:  "existing.txt",
		OldString: "world",
		NewString: "lenos",
	})
	require.False(t, resp.IsError, "relative path should be accepted: %s", resp.Content)
	require.Equal(t, "hello lenos", mustReadFile(t, testFile))

	// Relative path in subdirectory.
	subDir := filepath.Join(workingDir, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	subFile := filepath.Join(subDir, "file.txt")
	require.NoError(t, os.WriteFile(subFile, []byte("old content"), 0o644))
	ft.MarkAsRead("test-session", subFile)

	resp = runTool(t, tool, ctx, EditToolName, EditParams{
		FilePath:  "sub/file.txt",
		OldString: "old",
		NewString: "new",
	})
	require.False(t, resp.IsError, "subdirectory path should be accepted: %s", resp.Content)
	require.Equal(t, "new content", mustReadFile(t, subFile))
}

func TestEditTool_ContainedJoin_RejectsOutOfScopePaths(t *testing.T) {
	t.Parallel()
	workingDir := t.TempDir()

	tool := NewEditTool(nil, nil, nil, workingDir)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	for _, tc := range escapeCases {
		resp := runTool(t, tool, ctx, EditToolName, EditParams{
			FilePath:  tc.path,
			OldString: "old",
			NewString: "new",
		})
		require.True(t, resp.IsError, "%s should be rejected", tc.description)
		require.Contains(t, resp.Content, "escapes working directory")
	}
}

func TestEditTool_ContainedJoin_RejectionFiresBeforeFilesystemAccess(t *testing.T) {
	t.Parallel()
	workingDir := t.TempDir()

	// Create a file outside workingDir that the tool should NOT be able to access.
	outsidePath := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outsidePath, []byte("secret content"), 0o600))

	tool := NewEditTool(nil, nil, nil, workingDir)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	resp := runTool(t, tool, ctx, EditToolName, EditParams{
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

func TestEditTool_ContainedJoin_AcceptsAbsolutePathInsideWorkingDir(t *testing.T) {
	t.Parallel()
	workingDir := workingDirForTest(t.TempDir())
	ft := &testFiletracker{}

	// Pre-create a file.
	testFile := filepath.Join(workingDir, "absolute_inside.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("old content"), 0o644))
	ft.MarkAsRead("test-session", testFile)

	tool := NewEditTool(nil, &testHistory{}, ft, workingDir)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	absInside := filepath.Join(workingDir, "absolute_inside.txt")
	resp := runTool(t, tool, ctx, EditToolName, EditParams{
		FilePath:  absInside,
		OldString: "old",
		NewString: "new",
	})
	require.False(t, resp.IsError, "absolute path inside workingDir should be accepted: %s", resp.Content)
	require.Equal(t, "new content", mustReadFile(t, absInside))
}
