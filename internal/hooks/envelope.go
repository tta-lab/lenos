package hooks

import (
	"encoding/json"
	"time"

	"charm.land/fantasy"
)

// PostStepEvent is the stable contract written to stdin of "post_step" hooks.
// Schema version starts at 1; bump on breaking changes. The "event" field is a
// discriminator so future events (pre_step, session_end, etc.) can share the
// envelope shape.
type PostStepEvent struct {
	Version             int     `json:"version"`               // schema version (1 today)
	Event               string  `json:"event"`                 // "post_step"
	StepIndex           int     `json:"step_index"`            // 0-indexed step within the run loop
	SessionID           string  `json:"session_id"`            // lenos session UUID
	ModelID             string  `json:"model_id"`              // model identifier
	ContextWindow       int     `json:"context_window"`        // model's context window in tokens
	InputTokens         int     `json:"input_tokens"`          // prompt+input tokens for the just-completed step
	OutputTokens        int     `json:"output_tokens"`         // completion tokens for the just-completed step
	TotalTokens         int     `json:"total_tokens"`          // total tokens (input + output)
	ReasoningTokens     int     `json:"reasoning_tokens"`      // reasoning/thinking tokens (0 if provider doesn't expose)
	CacheCreationTokens int     `json:"cache_creation_tokens"` // cache-creation tokens (0 if provider doesn't expose)
	CacheReadTokens     int     `json:"cache_read_tokens"`     // cache-hit tokens (0 if provider doesn't expose)
	ContextUsedPct      float64 `json:"context_used_pct"`      // (input+output)/context_window * 100
	ContextRemainingPct float64 `json:"context_remaining_pct"` // 100 - context_used_pct
	Timestamp           string  `json:"timestamp"`             // RFC3339 UTC
}

// MarshalPostStep builds the envelope from typed inputs. contextWindow=0 → both
// percentages are 0 (no divide-by-zero). Timestamp is the caller-provided `now`
// so tests can pin it.
func MarshalPostStep(stepIdx int, sessionID, modelID string, contextWindow int, u fantasy.Usage, now time.Time) ([]byte, error) {
	var usedPct, remainingPct float64
	if contextWindow > 0 {
		usedPct = float64(u.InputTokens+u.OutputTokens) / float64(contextWindow) * 100
		remainingPct = 100 - usedPct
	} else if contextWindow == 0 && (u.InputTokens > 0 || u.OutputTokens > 0) {
		// contextWindow is zero but tokens were consumed — percentage is meaningless.
		// Use -1 as a sentinel so consumers can distinguish "not available" from "truly 0%".
		usedPct = -1
		remainingPct = -1
	}
	ev := PostStepEvent{
		Version:             1,
		Event:               "post_step",
		StepIndex:           stepIdx,
		SessionID:           sessionID,
		ModelID:             modelID,
		ContextWindow:       contextWindow,
		InputTokens:         int(u.InputTokens),
		OutputTokens:        int(u.OutputTokens),
		TotalTokens:         int(u.TotalTokens),
		ReasoningTokens:     int(u.ReasoningTokens),
		CacheCreationTokens: int(u.CacheCreationTokens),
		CacheReadTokens:     int(u.CacheReadTokens),
		ContextUsedPct:      usedPct,
		ContextRemainingPct: remainingPct,
		Timestamp:           now.UTC().Format(time.RFC3339),
	}
	return json.Marshal(ev)
}
