package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"charm.land/fantasy"
	"github.com/tta-lab/lenos/internal/atif"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/version"
)

type trajectoryPathContextKey struct{}

type TrajectoryRecorder struct {
	mu       sync.Mutex
	path     string
	current  int
	traj     atif.Trajectory
	finished bool
}

func NewTrajectoryRecorder(path, sessionID string, model Model) *TrajectoryRecorder {
	return &TrajectoryRecorder{
		path:    path,
		current: -1,
		traj: atif.Trajectory{
			SchemaVersion: "ATIF-v1.7",
			TrajectoryID:  sessionID,
			SessionID:     sessionID,
			Agent: atif.Agent{
				Name:      "lenos",
				Version:   version.Version,
				ModelName: model.messageModelID(),
			},
		},
	}
}

func ContextWithTrajectoryPath(ctx context.Context, path string) context.Context {
	if path == "" {
		return ctx
	}
	return context.WithValue(ctx, trajectoryPathContextKey{}, path)
}

func TrajectoryPathFromContext(ctx context.Context) string {
	path, _ := ctx.Value(trajectoryPathContextKey{}).(string)
	return path
}

func (r *TrajectoryRecorder) UserMessage(ctx context.Context, text string) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.traj.Steps = append(r.traj.Steps, atif.Step{
		StepID:  r.nextStepIDLocked(),
		Source:  "user",
		Message: text,
	})
	r.current = -1
	return r.checkpointLocked()
}

func (r *TrajectoryRecorder) RuntimePrompt(ctx context.Context, text string, extra map[string]any) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if extra == nil {
		extra = map[string]any{}
	}
	if _, ok := extra["kind"]; !ok {
		extra["kind"] = "runtime_prompt"
	}
	if _, ok := extra["name"]; !ok {
		extra["name"] = "lenos_runtime"
	}
	r.traj.Steps = append(r.traj.Steps, atif.Step{
		StepID:  r.nextStepIDLocked(),
		Source:  "system",
		Message: text,
		Extra:   extra,
	})
	r.current = -1
	return r.checkpointLocked()
}

func (r *TrajectoryRecorder) AgentStep(ctx context.Context, msg message.Message, usage fantasy.Usage, costUSD float64) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	metrics := metricsFromUsage(usage, costUSD)
	r.traj.Steps = append(r.traj.Steps, atif.Step{
		StepID:           r.nextStepIDLocked(),
		Source:           "agent",
		Message:          msg.Content().String(),
		ReasoningContent: msg.ReasoningContent().String(),
		ModelName:        msg.Model,
		LLMCallCount:     1,
		Metrics:          &metrics,
	})
	r.current = len(r.traj.Steps) - 1
	return r.checkpointLocked()
}

func (r *TrajectoryRecorder) AttachRunObservation(ctx context.Context, cmd message.CommandContent, duration time.Duration, background bool, jobID string) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.current < 0 || r.current >= len(r.traj.Steps) {
		return r.checkpointLocked()
	}
	step := &r.traj.Steps[r.current]
	if step.Observation == nil {
		step.Observation = &atif.Observation{}
	}
	extra := map[string]any{
		"tool":             "run",
		"command":          cmd.Command,
		"pending":          cmd.Pending,
		"background":       background,
		"job_id":           nil,
		"output_truncated": false,
		"full_output_path": nil,
		"elapsed_ms":       duration.Milliseconds(),
	}
	if cmd.ExitCode != nil {
		extra["exit_code"] = *cmd.ExitCode
	}
	if jobID != "" {
		extra["job_id"] = jobID
	}
	step.Observation.Results = append(step.Observation.Results, atif.ObservationResult{
		Content: cmd.Observation,
		Extra:   extra,
	})
	return r.checkpointLocked()
}

func (r *TrajectoryRecorder) MarkInterrupted(ctx context.Context, reason string) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.traj.Extra == nil {
		r.traj.Extra = map[string]any{}
	}
	r.traj.Extra["interrupted"] = true
	r.traj.Extra["interrupt_reason"] = reason
	return r.checkpointLocked()
}

func (r *TrajectoryRecorder) Finish(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.finished = true
	return r.checkpointLocked()
}

func metricsFromUsage(usage fantasy.Usage, costUSD float64) atif.Metrics {
	cost := costUSD
	promptTokens := usage.InputTokens + usage.CacheReadTokens
	return atif.Metrics{
		PromptTokens:     promptTokens,
		CompletionTokens: usage.OutputTokens,
		CachedTokens:     usage.CacheReadTokens,
		CostUSD:          &cost,
		Extra: map[string]any{
			"reasoning_tokens":      usage.ReasoningTokens,
			"cache_creation_tokens": usage.CacheCreationTokens,
			"cache_miss_tokens":     usage.InputTokens + usage.CacheCreationTokens,
			"raw_input_tokens":      usage.InputTokens,
		},
	}
}

func (r *TrajectoryRecorder) nextStepIDLocked() int {
	return len(r.traj.Steps) + 1
}

func (r *TrajectoryRecorder) checkpointLocked() error {
	r.traj.FinalMetrics = finalMetrics(r.traj.Steps)
	return WriteTrajectoryFile(r.path, r.traj)
}

func finalMetrics(steps []atif.Step) atif.FinalMetrics {
	var totalCost float64
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
		final.TotalPromptTokens += step.Metrics.PromptTokens
		final.TotalCompletionTokens += step.Metrics.CompletionTokens
		final.TotalCachedTokens += step.Metrics.CachedTokens
		if step.Metrics.CostUSD != nil {
			totalCost += *step.Metrics.CostUSD
		}
		addExtraInt64(final.Extra, "reasoning_tokens", step.Metrics.Extra["reasoning_tokens"])
		addExtraInt64(final.Extra, "cache_creation_tokens", step.Metrics.Extra["cache_creation_tokens"])
		addExtraInt64(final.Extra, "cache_miss_tokens", step.Metrics.Extra["cache_miss_tokens"])
	}
	cost := totalCost
	final.TotalCostUSD = &cost
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

func WriteTrajectoryFile(path string, traj atif.Trajectory) error {
	if path == "" {
		return nil
	}

	data, err := json.Marshal(traj)
	if err != nil {
		return fmt.Errorf("write trajectory: marshal: %w", err)
	}
	data = append(data, '\n')

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

func recordTrajectoryPrompt(ctx context.Context, recorder *TrajectoryRecorder, prompt turnPrompt) error {
	role := prompt.Role
	if role == "" {
		role = message.User
	}
	if role == message.Runtime {
		return recorder.RuntimePrompt(ctx, prompt.Text, runtimePromptExtra(prompt.Text))
	}
	if role == message.User {
		return recorder.UserMessage(ctx, prompt.Text)
	}
	return nil
}

func runtimePromptExtra(text string) map[string]any {
	if strings.Contains(text, "background job completed") {
		return map[string]any{
			"kind":   "background_job_completed",
			"name":   "lenos_runtime",
			"job_id": extractBackgroundJobID(text),
		}
	}
	return nil
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
