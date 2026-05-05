package hooks

import (
	"encoding/json"
	"testing"
	"time"

	"charm.land/fantasy"
)

func TestMarshalPostStep_RoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 5, 8, 8, 0, 0, time.UTC)
	u := fantasy.Usage{
		InputTokens:         14523,
		OutputTokens:        421,
		TotalTokens:         14944,
		ReasoningTokens:     87,
		CacheCreationTokens: 0,
		CacheReadTokens:     8200,
	}

	data, err := MarshalPostStep(12, "01HZ-session-id", "claude-sonnet-4-5", 200000, u, now)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var ev PostStepEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if ev.Version != 1 {
		t.Fatalf("version = %d, want 1", ev.Version)
	}
	if ev.Event != "post_step" {
		t.Fatalf("event = %q, want post_step", ev.Event)
	}
	if ev.StepIndex != 12 {
		t.Fatalf("step_index = %d, want 12", ev.StepIndex)
	}
	if ev.SessionID != "01HZ-session-id" {
		t.Fatalf("session_id = %q", ev.SessionID)
	}
	if ev.ModelID != "claude-sonnet-4-5" {
		t.Fatalf("model_id = %q", ev.ModelID)
	}
	if ev.ContextWindow != 200000 {
		t.Fatalf("context_window = %d", ev.ContextWindow)
	}
	if ev.InputTokens != 14523 {
		t.Fatalf("input_tokens = %d", ev.InputTokens)
	}
	if ev.OutputTokens != 421 {
		t.Fatalf("output_tokens = %d", ev.OutputTokens)
	}
	if ev.TotalTokens != 14944 {
		t.Fatalf("total_tokens = %d", ev.TotalTokens)
	}
	if ev.ReasoningTokens != 87 {
		t.Fatalf("reasoning_tokens = %d", ev.ReasoningTokens)
	}
	if ev.CacheCreationTokens != 0 {
		t.Fatalf("cache_creation_tokens = %d", ev.CacheCreationTokens)
	}
	if ev.CacheReadTokens != 8200 {
		t.Fatalf("cache_read_tokens = %d", ev.CacheReadTokens)
	}
	if diff := ev.ContextUsedPct - 7.472; diff > 0.001 || diff < -0.001 {
		t.Fatalf("context_used_pct = %f, want 7.472", ev.ContextUsedPct)
	}
	if diff := ev.ContextRemainingPct - 92.528; diff > 0.001 || diff < -0.001 {
		t.Fatalf("context_remaining_pct = %f, want 92.528", ev.ContextRemainingPct)
	}
	if ev.Timestamp != "2026-05-05T08:08:00Z" {
		t.Fatalf("timestamp = %q", ev.Timestamp)
	}
}

func TestMarshalPostStep_ZeroContextWindow(t *testing.T) {
	t.Run("no tokens", func(t *testing.T) {
		now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
		u := fantasy.Usage{InputTokens: 0, OutputTokens: 0}

		data, err := MarshalPostStep(0, "sid", "model", 0, u, now)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var ev PostStepEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if ev.ContextUsedPct != 0 {
			t.Fatalf("context_used_pct = %f, want 0 (no tokens, no window)", ev.ContextUsedPct)
		}
		if ev.ContextRemainingPct != 0 {
			t.Fatalf("context_remaining_pct = %f, want 0", ev.ContextRemainingPct)
		}
	})

	t.Run("tokens no window", func(t *testing.T) {
		now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
		u := fantasy.Usage{InputTokens: 100, OutputTokens: 50}

		data, err := MarshalPostStep(0, "sid", "model", 0, u, now)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var ev PostStepEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if ev.ContextUsedPct != -1 {
			t.Fatalf("context_used_pct = %f, want -1 (sentinel for meaningless pct)", ev.ContextUsedPct)
		}
		if ev.ContextRemainingPct != -1 {
			t.Fatalf("context_remaining_pct = %f, want -1", ev.ContextRemainingPct)
		}
	})
}

func TestMarshalPostStep_TokenMath(t *testing.T) {
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	u := fantasy.Usage{InputTokens: 300, OutputTokens: 200}

	data, err := MarshalPostStep(0, "sid", "model", 1000, u, now)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var ev PostStepEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ev.ContextUsedPct != 50.0 {
		t.Fatalf("context_used_pct = %f, want 50.0", ev.ContextUsedPct)
	}
	if ev.ContextRemainingPct != 50.0 {
		t.Fatalf("context_remaining_pct = %f, want 50.0", ev.ContextRemainingPct)
	}
}

func TestMarshalPostStep_StableKeyOrder(t *testing.T) {
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	u := fantasy.Usage{InputTokens: 100, OutputTokens: 50}
	data1, err := MarshalPostStep(0, "sid", "model", 1000, u, now)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	data2, err := MarshalPostStep(0, "sid", "model", 1000, u, now)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data1) != string(data2) {
		t.Fatal("marshal output is not stable")
	}
}

func TestMarshalPostStep_AllFieldsInJSON(t *testing.T) {
	now := time.Date(2026, 5, 5, 8, 8, 0, 0, time.UTC)
	u := fantasy.Usage{
		InputTokens:         14523,
		OutputTokens:        421,
		TotalTokens:         14944,
		ReasoningTokens:     87,
		CacheCreationTokens: 100,
		CacheReadTokens:     8200,
	}

	data, err := MarshalPostStep(12, "01HZ-session", "claude-sonnet-4-5", 200000, u, now)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify all required fields from JSON schema are present
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	required := []string{
		"version", "event", "step_index", "session_id", "model_id",
		"context_window", "input_tokens", "output_tokens", "total_tokens",
		"reasoning_tokens", "cache_creation_tokens", "cache_read_tokens",
		"context_used_pct", "context_remaining_pct", "timestamp",
	}
	for _, field := range required {
		if _, ok := raw[field]; !ok {
			t.Fatalf("missing required field: %s", field)
		}
	}
}

func TestJSONSchemaFields(t *testing.T) {
	// Load the schema file and verify it matches the Go struct
	schema := struct {
		Required   []string       `json:"required"`
		Properties map[string]any `json:"properties"`
	}{}
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	now := time.Date(2026, 5, 5, 8, 8, 0, 0, time.UTC)
	u := fantasy.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150}
	data, err := MarshalPostStep(0, "sid", "model", 1000, u, now)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var ev map[string]any
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, field := range schema.Required {
		if _, ok := ev[field]; !ok {
			t.Fatalf("Go struct missing field required by schema: %s", field)
		}
	}

	// Verify no extra fields beyond properties
	for k := range ev {
		if _, ok := schema.Properties[k]; !ok && k != "$schema" {
			t.Fatalf("Go struct has field not in schema: %s", k)
		}
	}
}

// schemaJSON is the content of schema/post_step.v1.json embedded for testing.
const schemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://lenos/schemas/hooks/post_step.v1.json",
  "title": "PostStepEvent v1",
  "type": "object",
  "required": ["version", "event", "step_index", "session_id", "model_id", "context_window",
               "input_tokens", "output_tokens", "total_tokens", "reasoning_tokens",
               "cache_creation_tokens", "cache_read_tokens",
               "context_used_pct", "context_remaining_pct", "timestamp"],
  "properties": {
    "version": {"const": 1},
    "event": {"const": "post_step"},
    "step_index": {"type": "integer", "minimum": 0},
    "session_id": {"type": "string"},
    "model_id": {"type": "string"},
    "context_window": {"type": "integer", "minimum": 0},
    "input_tokens": {"type": "integer", "minimum": 0},
    "output_tokens": {"type": "integer", "minimum": 0},
    "total_tokens": {"type": "integer", "minimum": 0},
    "reasoning_tokens": {"type": "integer", "minimum": 0},
    "cache_creation_tokens": {"type": "integer", "minimum": 0},
    "cache_read_tokens": {"type": "integer", "minimum": 0},
    "context_used_pct": {"type": "number", "minimum": 0, "maximum": 100},
    "context_remaining_pct": {"type": "number", "minimum": 0, "maximum": 100},
    "timestamp": {"type": "string", "format": "date-time"}
  },
  "additionalProperties": false
}`
