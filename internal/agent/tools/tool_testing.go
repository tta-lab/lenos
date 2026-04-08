package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/filetracker"
	"github.com/tta-lab/lenos/internal/history"
	"github.com/tta-lab/lenos/internal/pubsub"
)

// workingDirForTest canonicalizes workingDir so that path lookups in the
// testFiletracker mock are consistent with ContainedJoin inside the tools.
func workingDirForTest(workingDir string) string {
	real, err := filepath.EvalSymlinks(workingDir)
	if err != nil {
		return workingDir
	}
	return real
}

// testFiletracker is a minimal mock for filetracker.Service used across containment tests.
// It stores exact paths (no relpath conversion) and returns zero time for any
// path not previously recorded.
type testFiletracker struct {
	reads map[string]time.Time
}

var _ filetracker.Service = (*testFiletracker)(nil)

func (m *testFiletracker) RecordRead(ctx context.Context, sessionID, path string) {
	if m.reads == nil {
		m.reads = make(map[string]time.Time)
	}
	m.reads[path] = time.Now()
}

func (m *testFiletracker) LastReadTime(ctx context.Context, sessionID, path string) time.Time {
	if t, ok := m.reads[path]; ok {
		return t
	}
	return time.Time{}
}

func (m *testFiletracker) ListReadFiles(ctx context.Context, sessionID string) ([]string, error) {
	var paths []string
	for p := range m.reads {
		paths = append(paths, p)
	}
	return paths, nil
}

// MarkAsRead records a path as having been read.
func (m *testFiletracker) MarkAsRead(sessionID, path string) {
	if m.reads == nil {
		m.reads = make(map[string]time.Time)
	}
	m.reads[path] = time.Now()
}

// testHistory is a minimal mock for history.Service used across containment tests.
type testHistory struct{}

var _ history.Service = (*testHistory)(nil)

func (m *testHistory) Subscribe(ctx context.Context) <-chan pubsub.Event[history.File] { //nolint:revive
	ch := make(chan pubsub.Event[history.File])
	close(ch)
	return ch
}

func (m *testHistory) Create(context.Context, string, string, string) (history.File, error) {
	return history.File{}, nil
}

func (m *testHistory) CreateVersion(context.Context, string, string, string) (history.File, error) {
	return history.File{}, nil
}
func (m *testHistory) Get(context.Context, string) (history.File, error) { return history.File{}, nil }
func (m *testHistory) GetByPathAndSession(context.Context, string, string) (history.File, error) {
	return history.File{}, nil
}
func (m *testHistory) ListBySession(context.Context, string) ([]history.File, error) { return nil, nil }
func (m *testHistory) ListLatestSessionFiles(context.Context, string) ([]history.File, error) {
	return nil, nil
}
func (m *testHistory) Delete(context.Context, string) error             { return nil }
func (m *testHistory) DeleteSessionFiles(context.Context, string) error { return nil }

// runTool invokes a tool with the given params and returns the response.
func runTool[T any](t *testing.T, tool fantasy.AgentTool, ctx context.Context, name string, params T) fantasy.ToolResponse {
	t.Helper()
	input, err := json.Marshal(params)
	require.NoError(t, err)
	call := fantasy.ToolCall{
		ID:    "test-call",
		Name:  name,
		Input: string(input),
	}
	resp, err := tool.Run(ctx, call)
	require.NoError(t, err)
	return resp
}

// escapePathCase describes an out-of-scope path that must be rejected.
type escapePathCase struct {
	path        string
	description string
}

// escapeCases holds the shared rejection scenarios used by both Write and Edit tools.
var escapeCases = []escapePathCase{
	{"/etc/passwd", "absolute path outside working directory"},
	{"../other/foo.txt", "relative path escaping via parent directory"},
	{"sub/../../etc/passwd", "deeply nested escape via parent directories"},
}
