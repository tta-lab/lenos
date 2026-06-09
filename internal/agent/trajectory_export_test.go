package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/message"
)

func TestExportTrajectoryFileStripsANSIAndAddsAgentVersion(t *testing.T) {
	t.Parallel()

	exitCode := 0
	path := filepath.Join(t.TempDir(), "trajectory.json")
	err := ExportTrajectoryFile(path, "session-1", "test-model", []message.Message{
		{ID: "m1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "\x1b[31mhello\x1b[m"}}},
		{ID: "m2", Role: message.Assistant, Model: "test-model", Provider: "mock", Parts: []message.ContentPart{message.TextContent{Text: "hi"}}},
		{ID: "m3", Role: message.Result, Parts: []message.ContentPart{message.CommandContent{Command: "\x1b[31mecho ok\x1b[m", Output: "\x1b[32mok\x1b[m\n", ExitCode: &exitCode}}},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(data), "\x1b")

	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	agent := got["agent"].(map[string]any)
	require.Equal(t, "lenos", agent["name"])
	require.NotEmpty(t, agent["version"])

	steps := got["steps"].([]any)
	require.Len(t, steps, 3)
	for i, step := range steps {
		require.Equal(t, float64(i+1), step.(map[string]any)["step_id"])
	}
	require.Equal(t, "hello", steps[0].(map[string]any)["message"])
	observation := steps[2].(map[string]any)["observation"].(map[string]any)
	results := observation["results"].([]any)
	require.Equal(t, "ok\n", results[0].(map[string]any)["content"])
	require.Equal(t, "echo ok", results[0].(map[string]any)["source_call_id"])
}
