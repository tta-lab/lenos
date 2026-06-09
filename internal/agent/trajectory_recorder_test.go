package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/message"
)

func TestTrajectoryRecorderMetricsAndObservation(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "trajectory.json")
	recorder := NewTrajectoryRecorder(path, "session-1", trajectoryTestModel())
	require.NoError(t, recorder.UserMessage(t.Context(), "Start."))
	require.NoError(t, recorder.RuntimePrompt(t.Context(), "runtime", nil))

	msg := message.Message{
		Role:     message.Assistant,
		Model:    "gpt-test",
		Parts:    []message.ContentPart{message.TextContent{Text: "Running\n<run>\necho ok\n</run>"}},
		Provider: "test",
	}
	require.NoError(t, recorder.AgentStep(t.Context(), msg, fantasy.Usage{
		InputTokens:         10,
		CacheReadTokens:     4,
		CacheCreationTokens: 3,
		OutputTokens:        5,
		ReasoningTokens:     2,
	}, 0.25))

	exitCode := 0
	require.NoError(t, recorder.AttachRunObservation(t.Context(), message.CommandContent{
		Command:     "echo ok",
		Observation: "ok",
		ExitCode:    &exitCode,
	}, 1200*time.Millisecond, false, ""))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))

	require.Equal(t, "ATIF-v1.7", got["schema_version"])
	steps := got["steps"].([]any)
	require.Len(t, steps, 3)
	require.Equal(t, "user", steps[0].(map[string]any)["source"])
	require.Equal(t, "system", steps[1].(map[string]any)["source"])
	require.Equal(t, "runtime_prompt", steps[1].(map[string]any)["extra"].(map[string]any)["kind"])

	agentStep := steps[2].(map[string]any)
	require.Equal(t, "agent", agentStep["source"])
	metrics := agentStep["metrics"].(map[string]any)
	require.Equal(t, float64(14), metrics["prompt_tokens"])
	require.Equal(t, float64(5), metrics["completion_tokens"])
	require.Equal(t, float64(4), metrics["cached_tokens"])
	require.Equal(t, 0.25, metrics["cost_usd"])
	require.Equal(t, float64(13), metrics["extra"].(map[string]any)["cache_miss_tokens"])

	result := agentStep["observation"].(map[string]any)["results"].([]any)[0].(map[string]any)
	require.Equal(t, "ok", result["content"])
	extra := result["extra"].(map[string]any)
	require.Equal(t, "run", extra["tool"])
	require.Equal(t, "echo ok", extra["command"])
	require.Equal(t, float64(0), extra["exit_code"])
	require.Equal(t, false, extra["pending"])
	require.Equal(t, false, extra["background"])
	require.Equal(t, float64(1200), extra["elapsed_ms"])

	final := got["final_metrics"].(map[string]any)
	require.Equal(t, float64(14), final["total_prompt_tokens"])
	require.Equal(t, float64(5), final["total_completion_tokens"])
	require.Equal(t, float64(4), final["total_cached_tokens"])
	require.Equal(t, float64(3), final["total_steps"])
	require.Equal(t, float64(19), final["extra"].(map[string]any)["total_tokens"])
}

func TestTrajectoryRecorderMarksInterrupted(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "trajectory.json")
	recorder := NewTrajectoryRecorder(path, "session-1", trajectoryTestModel())
	require.NoError(t, recorder.MarkInterrupted(t.Context(), "signal"))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))

	extra := got["extra"].(map[string]any)
	require.Equal(t, true, extra["interrupted"])
	require.Equal(t, "signal", extra["interrupt_reason"])
}

func trajectoryTestModel() Model {
	return Model{
		ModelCfg: config.SelectedModel{
			Model: "gpt-test",
		},
	}
}
