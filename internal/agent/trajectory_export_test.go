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
		{ID: "m2", Role: message.Assistant, Model: "test-model", Provider: "mock", Parts: []message.ContentPart{message.TextContent{Text: "\x3crun>\necho ok\n\x3c/run>"}}},
		{ID: "m3", Role: message.Result, Parts: []message.ContentPart{message.CommandContent{Command: "\x1b[31mecho ok\x1b[m", Output: "\x1b[32mok\x1b[m\n", ExitCode: &exitCode}}},
	}, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(data), "\x1b")
	require.Contains(t, string(data), "\x3crun>")
	require.NotContains(t, string(data), "\\u003c")

	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	agent := got["agent"].(map[string]any)
	require.Equal(t, "lenos", agent["name"])
	require.NotEmpty(t, agent["version"])

	steps := got["steps"].([]any)
	require.Len(t, steps, 2)
	for i, step := range steps {
		require.Equal(t, float64(i+1), step.(map[string]any)["step_id"])
		require.NotEmpty(t, step.(map[string]any)["message"])
	}
	require.Equal(t, "hello", steps[0].(map[string]any)["message"])
	agentStep := steps[1].(map[string]any)
	require.Equal(t, "agent", agentStep["source"])
	observation := agentStep["observation"].(map[string]any)
	results := observation["results"].([]any)
	require.Equal(t, "ok\n", results[0].(map[string]any)["content"])
	require.NotContains(t, results[0].(map[string]any), "source_call_id")
}

func TestExportTrajectoryFileKeepsBackgroundCompletionAsSystemStep(t *testing.T) {
	t.Parallel()

	exitCode := 0
	path := filepath.Join(t.TempDir(), "trajectory.json")
	err := ExportTrajectoryFile(path, "session-1", "test-model", []message.Message{
		{ID: "m1", Role: message.Assistant, Model: "test-model", Parts: []message.ContentPart{message.TextContent{Text: "\x3crun>\nlong command\n\x3c/run>"}}},
		{ID: "m2", Role: message.Result, Parts: []message.ContentPart{message.CommandContent{Command: "long command", Output: "background job started (job_id: job-1).", ExitCode: &exitCode}}},
		{ID: "m3", Role: message.Result, Parts: []message.ContentPart{message.CommandContent{Command: "long command", Output: "background job completed (job_id: job-1)\n\nok\n", ExitCode: &exitCode}}},
	}, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	steps := got["steps"].([]any)
	require.Len(t, steps, 2)

	agentStep := steps[0].(map[string]any)
	require.Equal(t, "agent", agentStep["source"])
	require.NotEmpty(t, agentStep["message"])
	agentResults := agentStep["observation"].(map[string]any)["results"].([]any)
	require.Equal(t, "background job started (job_id: job-1).", agentResults[0].(map[string]any)["content"])

	systemStep := steps[1].(map[string]any)
	require.Equal(t, "system", systemStep["source"])
	require.Equal(t, "Background job completed.", systemStep["message"])
	systemResults := systemStep["observation"].(map[string]any)["results"].([]any)
	require.Contains(t, systemResults[0].(map[string]any)["content"], "ok")
	require.Equal(t, "background_job_completed", systemStep["extra"].(map[string]any)["kind"])
	require.Equal(t, "job-1", systemResults[0].(map[string]any)["extra"].(map[string]any)["job_id"])
}
