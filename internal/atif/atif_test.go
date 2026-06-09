package atif

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrajectoryJSONFields(t *testing.T) {
	t.Parallel()

	cost := 0.25
	traj := Trajectory{
		SchemaVersion: "ATIF-v1.7",
		TrajectoryID:  "session-1",
		SessionID:     "session-1",
		Agent: Agent{
			Name:      "lenos",
			Version:   "devel",
			ModelName: "gpt-test",
		},
		Steps: []Step{
			{
				Source:           "agent",
				Message:          "hello",
				ReasoningContent: "thinking",
				ModelName:        "gpt-test",
				LLMCallCount:     1,
				Observation: &Observation{
					Results: []ObservationResult{
						{
							Content: "ok",
							Extra: map[string]any{
								"tool":      "run",
								"command":   "echo ok",
								"exit_code": float64(0),
								"pending":   false,
							},
						},
					},
				},
				Metrics: &Metrics{
					PromptTokens:     12,
					CompletionTokens: 3,
					CachedTokens:     4,
					CostUSD:          &cost,
					Extra: map[string]any{
						"reasoning_tokens": float64(2),
					},
				},
			},
		},
		FinalMetrics: FinalMetrics{
			TotalPromptTokens:     12,
			TotalCompletionTokens: 3,
			TotalCachedTokens:     4,
			TotalCostUSD:          &cost,
			TotalSteps:            1,
		},
	}

	data, err := json.MarshalIndent(traj, "", "  ")
	require.NoError(t, err)
	require.JSONEq(t, `{
		"schema_version": "ATIF-v1.7",
		"trajectory_id": "session-1",
		"session_id": "session-1",
		"agent": {
			"name": "lenos",
			"version": "devel",
			"model_name": "gpt-test"
		},
		"steps": [
			{
				"source": "agent",
				"message": "hello",
				"reasoning_content": "thinking",
				"model_name": "gpt-test",
				"llm_call_count": 1,
				"observation": {
					"results": [
						{
							"content": "ok",
							"extra": {
								"command": "echo ok",
								"exit_code": 0,
								"pending": false,
								"tool": "run"
							}
						}
					]
				},
				"metrics": {
					"prompt_tokens": 12,
					"completion_tokens": 3,
					"cached_tokens": 4,
					"cost_usd": 0.25,
					"extra": {
						"reasoning_tokens": 2
					}
				}
			}
		],
		"final_metrics": {
			"total_prompt_tokens": 12,
			"total_completion_tokens": 3,
			"total_cached_tokens": 4,
			"total_cost_usd": 0.25,
			"total_steps": 1
		}
	}`, string(data))
}
