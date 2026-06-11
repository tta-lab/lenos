package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/atif"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/session"
)

func TestFinalMetrics_SessionTotalsExportTotalsNotLatestContext(t *testing.T) {
	t.Parallel()

	// Session with accumulated totals differing from context fields.
	cost := 1.25
	sess := &session.Session{
		ID:                    "session-1",
		TotalPromptTokens:     1000,
		TotalCompletionTokens: 500,
		TotalReasoningTokens:  75,
		CacheReadTokens:       200,
		CacheCreationTokens:   50,
		CacheMissTokens:       800, // InputTokens only (no CacheCreationTokens)
		Cost:                  cost,
		// Context fields are latest-turn only (different from totals).
		PromptTokens:     150,
		CompletionTokens: 60,
	}

	steps := []atif.Step{
		{
			StepID:  1,
			Source:  "agent",
			Message: "step 1",
		},
		{
			StepID:  2,
			Source:  "agent",
			Message: "step 2",
		},
	}

	result := finalMetrics(steps, sess)

	require.Equal(t, int64(1000), result.TotalPromptTokens,
		"TotalPromptTokens should be the session lifetime total")
	require.Equal(t, int64(500), result.TotalCompletionTokens,
		"TotalCompletionTokens should be the session lifetime total")
	require.Equal(t, int64(200), result.TotalCachedTokens,
		"TotalCachedTokens should be cache_read_tokens")
	require.Equal(t, 2, result.TotalSteps)

	require.NotNil(t, result.TotalCostUSD)
	require.Equal(t, 1.25, *result.TotalCostUSD)

	require.Equal(t, int64(75), result.Extra["reasoning_tokens"],
		"extra.reasoning_tokens should use session lifetime total")
	require.Equal(t, int64(50), result.Extra["cache_creation_tokens"],
		"extra.cache_creation_tokens should be the session value")
	require.Equal(t, int64(800), result.Extra["cache_miss_tokens"],
		"extra.cache_miss_tokens = sess.CacheMissTokens (InputTokens only)")
	require.Equal(t, int64(1500), result.Extra["total_tokens"],
		"extra.total_tokens = total_prompt_tokens + total_completion_tokens")
}

func TestFinalMetrics_CacheMissTokensCleanDerivation(t *testing.T) {
	t.Parallel()

	// total_prompt_tokens = non-cached input + cached input
	// cache_read_tokens = cached input
	// cache_miss_tokens = total_prompt_tokens - cache_read_tokens = non-cached input
	sess := &session.Session{
		ID:                    "session-2",
		TotalPromptTokens:     800,
		TotalCompletionTokens: 400,
		CacheReadTokens:       200,
		CacheMissTokens:       600,
		Cost:                  0.75,
	}

	steps := []atif.Step{
		{StepID: 1, Source: "agent", Message: "step"},
	}

	result := finalMetrics(steps, sess)

	require.Equal(t, int64(800), result.TotalPromptTokens)
	require.Equal(t, int64(200), result.TotalCachedTokens)
	require.Equal(t, int64(600), result.Extra["cache_miss_tokens"],
		"extra.cache_miss_tokens = sess.CacheMissTokens (InputTokens only)")

	// Verify cache_miss_tokens is what was actually non-cached input.
	require.Equal(t, int64(800-200), result.Extra["cache_miss_tokens"])
}

func TestFinalMetrics_NilSessionUsesStepAccumulation(t *testing.T) {
	t.Parallel()

	// Without a session, totals come from steps.
	steps := []atif.Step{
		{
			StepID:  1,
			Source:  "agent",
			Message: "step 1",
			Metrics: &atif.Metrics{
				PromptTokens:     100,
				CompletionTokens: 50,
				CachedTokens:     20,
				Extra: map[string]any{
					"reasoning_tokens":      int64(10),
					"cache_creation_tokens": int64(5),
					"cache_miss_tokens":     int64(80),
				},
			},
		},
		{
			StepID:  2,
			Source:  "agent",
			Message: "step 2",
			Metrics: &atif.Metrics{
				PromptTokens:     200,
				CompletionTokens: 100,
				CachedTokens:     40,
				Extra: map[string]any{
					"reasoning_tokens":      int64(20),
					"cache_creation_tokens": int64(10),
					"cache_miss_tokens":     int64(160),
				},
			},
		},
	}

	result := finalMetrics(steps, nil)

	require.Equal(t, int64(300), result.TotalPromptTokens)
	require.Equal(t, int64(150), result.TotalCompletionTokens)
	require.Equal(t, int64(60), result.TotalCachedTokens)
	require.Equal(t, int64(30), result.Extra["reasoning_tokens"])
	require.Equal(t, int64(15), result.Extra["cache_creation_tokens"])
	require.Equal(t, int64(240), result.Extra["cache_miss_tokens"])

	// Cost defaults to zero when no session.
	require.NotNil(t, result.TotalCostUSD)
	require.Equal(t, 0.0, *result.TotalCostUSD)
}

func TestFinalMetrics_ExportedTrajectoryUsesSessionTotals(t *testing.T) {
	t.Parallel()

	cost := 0.42
	exitCode := 0
	sess := &session.Session{
		ID:                    "session-export-1",
		TotalPromptTokens:     600,
		TotalCompletionTokens: 300,
		TotalReasoningTokens:  25,
		CacheReadTokens:       120,
		CacheCreationTokens:   15,
		CacheMissTokens:       480,
		Cost:                  cost,
	}

	msgs := []message.Message{
		{ID: "m1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "task"}}},
		{ID: "m2", Role: message.Assistant, Model: "test-model", Provider: "mock", Parts: []message.ContentPart{message.TextContent{Text: "<run>\necho ok\n</run>"}}},
		{ID: "m3", Role: message.Result, Parts: []message.ContentPart{message.CommandContent{Command: "echo ok", Output: "ok\n", ExitCode: &exitCode}}},
	}

	path := filepath.Join(t.TempDir(), "trajectory.json")
	err := ExportTrajectoryFile(path, "session-export-1", "test-model", msgs, sess)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))

	fm := got["final_metrics"].(map[string]any)
	require.Equal(t, float64(600), fm["total_prompt_tokens"],
		"total_prompt_tokens should be session lifetime total")
	require.Equal(t, float64(300), fm["total_completion_tokens"],
		"total_completion_tokens should be session lifetime total")
	require.Equal(t, float64(120), fm["total_cached_tokens"])
	require.Equal(t, float64(2), fm["total_steps"])
	require.Equal(t, 0.42, fm["total_cost_usd"])

	extra := fm["extra"].(map[string]any)
	require.Equal(t, float64(25), extra["reasoning_tokens"])
	require.Equal(t, float64(15), extra["cache_creation_tokens"])
	require.Equal(t, float64(480), extra["cache_miss_tokens"],
		"extra.cache_miss_tokens = sess.CacheMissTokens (InputTokens only)")
	require.Equal(t, float64(900), extra["total_tokens"])
}
