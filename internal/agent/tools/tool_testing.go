package tools

import (
	"context"
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

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
