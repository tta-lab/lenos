package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/tta-lab/lenos/internal/atif"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/version"
)

// trajectoryPathContextKey is the context key for --trajectory-json path.
type trajectoryPathContextKey struct{}

// ContextWithTrajectoryPath stores a trajectory output path in ctx.
func ContextWithTrajectoryPath(ctx context.Context, path string) context.Context {
	if path == "" {
		return ctx
	}
	return context.WithValue(ctx, trajectoryPathContextKey{}, path)
}

// TrajectoryPathFromContext returns the trajectory path from ctx, if set.
func TrajectoryPathFromContext(ctx context.Context) string {
	path, _ := ctx.Value(trajectoryPathContextKey{}).(string)
	return path
}

// ExportTrajectoryFile builds an ATIF trajectory from DB messages and session
// and writes it to path. When sess is nil, final_metrics are computed from
// step-level metrics only.
func ExportTrajectoryFile(path, sessionID, modelName string, messages []message.Message, sess *session.Session) error {
	return WriteTrajectoryFile(path, TrajectoryFromMessages(sessionID, modelName, messages, sess))
}

// TrajectoryFromMessages converts DB messages into an ATIF trajectory.
// When sess is non-nil, final_metrics are populated from the session's
// cumulative token accounting fields.
func TrajectoryFromMessages(sessionID, modelName string, messages []message.Message, sess *session.Session) atif.Trajectory {
	traj := atif.Trajectory{
		SchemaVersion: "ATIF-v1.7",
		TrajectoryID:  sessionID,
		SessionID:     sessionID,
		Agent: atif.Agent{
			Name:      "lenos",
			Version:   version.Version,
			ModelName: modelName,
		},
	}

	for _, msg := range messages {
		step, ok := StepFromMessage(msg)
		if !ok {
			continue
		}
		if msg.Role == message.Result && !isBackgroundCompletionStep(step) {
			attached := attachObservationToLastAgentStep(&traj, step)
			if attached {
				continue
			}
		}
		step.StepID = len(traj.Steps) + 1
		traj.Steps = append(traj.Steps, step)
	}
	traj.FinalMetrics = finalMetrics(traj.Steps, sess)
	return traj
}

func StepFromMessage(msg message.Message) (atif.Step, bool) {
	switch msg.Role {
	case message.User:
		content := cleanTrajectoryText(msg.Content().Text)
		if content == "" {
			return atif.Step{}, false
		}
		return atif.Step{
			Source:  "user",
			Message: content,
			Extra: map[string]any{
				"message_id": msg.ID,
			},
		}, true
	case message.System, message.Runtime:
		content := cleanTrajectoryText(msg.Content().Text)
		if content == "" {
			return atif.Step{}, false
		}
		return atif.Step{
			Source:  "system",
			Message: content,
			Extra: map[string]any{
				"message_id": msg.ID,
			},
		}, true
	case message.Assistant:
		content := cleanTrajectoryText(msg.Content().Text)
		if content == "" {
			return atif.Step{}, false
		}
		step := atif.Step{
			Source:    "agent",
			Message:   content,
			ModelName: msg.Model,
			Extra: map[string]any{
				"message_id": msg.ID,
			},
		}
		if msg.Provider != "" {
			step.Extra["provider"] = msg.Provider
		}
		return step, true
	case message.Result:
		command := msg.CommandContent()
		if command.Command == "" {
			return atif.Step{}, false
		}
		content := cleanTrajectoryText(command.Output)
		if command.Observation != "" {
			content = cleanTrajectoryText(command.Observation)
		}
		if content == "" {
			content = cleanTrajectoryText(command.String())
		}
		cleanCommand := cleanTrajectoryText(command.Command)
		extra := map[string]any{
			"message_id": msg.ID,
			"command":    cleanCommand,
			"pending":    command.Pending,
		}
		if command.ExitCode != nil {
			extra["exit_code"] = *command.ExitCode
		}
		kind := "result"
		stepMessage := "Command result."
		if backgroundKind := backgroundKindFromText(content); backgroundKind != "" {
			kind = backgroundKind
			stepMessage = backgroundRuntimeMessage(backgroundKind)
			extra["background"] = true
			extra["job_id"] = extractBackgroundJobID(content)
		}
		return atif.Step{
			Source:  "system",
			Message: stepMessage,
			Observation: &atif.Observation{
				Results: []atif.ObservationResult{{
					Content: content,
					Extra:   extra,
				}},
			},
			Extra: map[string]any{
				"message_id": msg.ID,
				"kind":       kind,
			},
		}, true
	default:
		return atif.Step{}, false
	}
}

func attachObservationToLastAgentStep(traj *atif.Trajectory, step atif.Step) bool {
	if step.Observation == nil {
		return false
	}
	for i := len(traj.Steps) - 1; i >= 0; i-- {
		if traj.Steps[i].Source != "agent" {
			continue
		}
		if traj.Steps[i].Observation == nil {
			traj.Steps[i].Observation = &atif.Observation{}
		}
		traj.Steps[i].Observation.Results = append(traj.Steps[i].Observation.Results, step.Observation.Results...)
		return true
	}
	return false
}

func isBackgroundCompletionStep(step atif.Step) bool {
	if step.Extra == nil {
		return false
	}
	kind, _ := step.Extra["kind"].(string)
	return isBackgroundRuntimeKind(kind)
}

func backgroundKindFromText(text string) string {
	switch {
	case strings.Contains(text, "background job completed"):
		return "background_job_completed"
	case strings.Contains(text, "background job killed"):
		return "background_job_killed"
	default:
		return ""
	}
}

func isBackgroundRuntimeKind(kind string) bool {
	return kind == "background_job_completed" || kind == "background_job_killed"
}

func cleanTrajectoryText(text string) string {
	return ansi.Strip(text)
}

func backgroundRuntimeMessage(kind string) string {
	if kind == "background_job_killed" {
		return "Background job killed."
	}
	return "Background job completed."
}

func extractBackgroundJobID(text string) any {
	const marker = "(job_id: "
	start := strings.Index(text, marker)
	if start < 0 {
		return nil
	}
	rest := text[start+len(marker):]
	end := strings.Index(rest, ")")
	if end < 0 {
		return nil
	}
	return rest[:end]
}

// finalMetrics computes final_metrics. When sess is non-nil, cumulative token
// accounting from the sessions table is used for totals; step-level per-turn
// metrics still contribute to reasoning/cache breakdowns.
func finalMetrics(steps []atif.Step, sess *session.Session) atif.FinalMetrics {
	final := atif.FinalMetrics{
		TotalSteps: len(steps),
		Extra: map[string]any{
			"reasoning_tokens":      int64(0),
			"cache_creation_tokens": int64(0),
			"cache_miss_tokens":     int64(0),
			"total_tokens":          int64(0),
		},
	}
	for _, step := range steps {
		if step.Metrics == nil {
			continue
		}
		if sess == nil {
			final.TotalPromptTokens += step.Metrics.PromptTokens
			final.TotalCompletionTokens += step.Metrics.CompletionTokens
			final.TotalCachedTokens += step.Metrics.CachedTokens
		}
		if step.Metrics.CostUSD != nil {
			if final.TotalCostUSD == nil {
				final.TotalCostUSD = new(float64)
			}
			*final.TotalCostUSD += *step.Metrics.CostUSD
		}
		addExtraInt64(final.Extra, "reasoning_tokens", step.Metrics.Extra["reasoning_tokens"])
		addExtraInt64(final.Extra, "cache_creation_tokens", step.Metrics.Extra["cache_creation_tokens"])
		addExtraInt64(final.Extra, "cache_miss_tokens", step.Metrics.Extra["cache_miss_tokens"])
	}
	if sess != nil {
		rawInput := sess.CacheMissTokens - sess.CacheCreationTokens
		final.TotalPromptTokens = rawInput + sess.CacheReadTokens
		final.TotalCompletionTokens = 0 // Not cumulative in current schema.
		final.TotalCachedTokens = sess.CacheReadTokens
		cost := sess.Cost
		final.TotalCostUSD = &cost
		final.Extra["cache_creation_tokens"] = sess.CacheCreationTokens
		final.Extra["cache_miss_tokens"] = sess.CacheMissTokens
	}
	if final.TotalCostUSD == nil {
		zeroCost := 0.0
		final.TotalCostUSD = &zeroCost
	}
	final.Extra["total_tokens"] = final.TotalPromptTokens + final.TotalCompletionTokens
	return final
}

func addExtraInt64(extra map[string]any, key string, value any) {
	current, _ := extra[key].(int64)
	switch v := value.(type) {
	case int64:
		extra[key] = current + v
	case int:
		extra[key] = current + int64(v)
	}
}

// WriteTrajectoryFile writes a trajectory to path atomically (temp + rename).
func WriteTrajectoryFile(path string, traj atif.Trajectory) error {
	if path == "" {
		return nil
	}

	var buf strings.Builder
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(traj); err != nil {
		return fmt.Errorf("write trajectory: marshal: %w", err)
	}
	data := []byte(buf.String())

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".trajectory-*.tmp")
	if err != nil {
		return fmt.Errorf("write trajectory: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write trajectory: write temp: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write trajectory: chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write trajectory: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("write trajectory: rename temp: %w", err)
	}
	keep = true
	return nil
}
