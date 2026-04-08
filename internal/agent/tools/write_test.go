package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/filetracker"
	"github.com/tta-lab/lenos/internal/history"
	"github.com/tta-lab/lenos/internal/pubsub"
)

type writeTestFiletracker struct {
	reads map[string]time.Time
}

var _ filetracker.Service = (*writeTestFiletracker)(nil)

func (m *writeTestFiletracker) RecordRead(ctx context.Context, sessionID, path string) {
	if m.reads == nil {
		m.reads = make(map[string]time.Time)
	}
	m.reads[path] = time.Now()
}

func (m *writeTestFiletracker) LastReadTime(ctx context.Context, sessionID, path string) time.Time {
	if t, ok := m.reads[path]; ok {
		return t
	}
	return time.Time{}
}

func (m *writeTestFiletracker) ListReadFiles(ctx context.Context, sessionID string) ([]string, error) {
	var paths []string
	for p := range m.reads {
		paths = append(paths, p)
	}
	return paths, nil
}

type writeTestHistory struct{}

var _ history.Service = (*writeTestHistory)(nil)

func (m *writeTestHistory) Subscribe(ctx context.Context) <-chan pubsub.Event[history.File] { //nolint:revive
	ch := make(chan pubsub.Event[history.File])
	close(ch)
	return ch
}

func (m *writeTestHistory) Create(ctx context.Context, sessionID, path, content string) (history.File, error) {
	return history.File{}, nil
}

func (m *writeTestHistory) CreateVersion(ctx context.Context, sessionID, path, content string) (history.File, error) {
	return history.File{}, nil
}

func (m *writeTestHistory) Get(ctx context.Context, id string) (history.File, error) {
	return history.File{}, nil
}

func (m *writeTestHistory) GetByPathAndSession(ctx context.Context, path, sessionID string) (history.File, error) {
	return history.File{}, nil
}

func (m *writeTestHistory) ListBySession(ctx context.Context, sessionID string) ([]history.File, error) {
	return nil, nil
}

func (m *writeTestHistory) ListLatestSessionFiles(ctx context.Context, sessionID string) ([]history.File, error) {
	return nil, nil
}
func (m *writeTestHistory) Delete(ctx context.Context, id string) error { return nil }
func (m *writeTestHistory) DeleteSessionFiles(ctx context.Context, sessionID string) error {
	return nil
}

func runWriteTool(t *testing.T, tool fantasy.AgentTool, ctx context.Context, params WriteParams) fantasy.ToolResponse {
	t.Helper()
	input, err := json.Marshal(params)
	require.NoError(t, err)
	call := fantasy.ToolCall{
		ID:    "test-write-call",
		Name:  WriteToolName,
		Input: string(input),
	}
	resp, err := tool.Run(ctx, call)
	require.NoError(t, err)
	return resp
}

func TestWriteTool_ContainedJoin_AcceptsInScopePaths(t *testing.T) {
	t.Parallel()
	workingDir := t.TempDir()

	tool := NewWriteTool(nil, &writeTestHistory{}, &writeTestFiletracker{}, workingDir)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	// Relative path in root.
	resp := runWriteTool(t, tool, ctx, WriteParams{
		FilePath: "foo.txt",
		Content:  "hello",
	})
	require.False(t, resp.IsError, "relative path should be accepted: %s", resp.Content)

	// Relative path in subdirectory.
	resp = runWriteTool(t, tool, ctx, WriteParams{
		FilePath: "sub/dir/bar.txt",
		Content:  "world",
	})
	require.False(t, resp.IsError, "subdirectory path should be accepted: %s", resp.Content)
}

func TestWriteTool_ContainedJoin_RejectsOutOfScopePaths(t *testing.T) {
	t.Parallel()
	workingDir := t.TempDir()

	tool := NewWriteTool(nil, nil, nil, workingDir)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	// Absolute path outside working directory.
	resp := runWriteTool(t, tool, ctx, WriteParams{
		FilePath: "/etc/passwd",
		Content:  "malicious",
	})
	require.True(t, resp.IsError, "absolute path outside CWD should be rejected")
	require.Contains(t, resp.Content, "escapes working directory")

	// Relative path escaping via parent directory.
	resp = runWriteTool(t, tool, ctx, WriteParams{
		FilePath: "../other/foo.txt",
		Content:  "malicious",
	})
	require.True(t, resp.IsError, "path with .. should be rejected")
	require.Contains(t, resp.Content, "escapes working directory")

	// Deeply nested escape.
	resp = runWriteTool(t, tool, ctx, WriteParams{
		FilePath: "sub/../../etc/passwd",
		Content:  "malicious",
	})
	require.True(t, resp.IsError, "deeply nested escape should be rejected")
	require.Contains(t, resp.Content, "escapes working directory")
}

func TestWriteTool_ContainedJoin_RejectionFiresBeforeFilesystemAccess(t *testing.T) {
	t.Parallel()
	workingDir := t.TempDir()

	// Create a file outside workingDir that the tool should NOT be able to write.
	outsidePath := filepath.Join(t.TempDir(), "should_not_exist.txt")

	tool := NewWriteTool(nil, nil, nil, workingDir)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	// Attempt to escape to a sibling directory.
	resp := runWriteTool(t, tool, ctx, WriteParams{
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

	tool := NewWriteTool(nil, &writeTestHistory{}, &writeTestFiletracker{}, workingDir)
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	absInside := filepath.Join(workingDir, "absolute_inside.txt")
	resp := runWriteTool(t, tool, ctx, WriteParams{
		FilePath: absInside,
		Content:  "absolute but inside",
	})
	require.False(t, resp.IsError, "absolute path inside workingDir should be accepted: %s", resp.Content)
}
