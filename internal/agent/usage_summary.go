package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"charm.land/fantasy"
)

type runUsageSummaryContextKey struct{}

type RunUsageSummary struct {
	Version              int     `json:"version"`
	Event                string  `json:"event"`
	SessionID            string  `json:"session_id"`
	ProviderID           string  `json:"provider_id"`
	ModelID              string  `json:"model_id"`
	InputTokens          int64   `json:"input_tokens"`
	RawInputTokens       int64   `json:"raw_input_tokens"`
	InputCacheHitTokens  int64   `json:"input_cache_hit_tokens"`
	InputCacheMissTokens int64   `json:"input_cache_miss_tokens"`
	CacheCreationTokens  int64   `json:"cache_creation_tokens"`
	CacheReadTokens      int64   `json:"cache_read_tokens"`
	OutputTokens         int64   `json:"output_tokens"`
	ReasoningTokens      int64   `json:"reasoning_tokens"`
	TotalTokens          int64   `json:"total_tokens"`
	CostUSD              float64 `json:"cost_usd"`
	StartedAt            string  `json:"started_at"`
	FinishedAt           string  `json:"finished_at"`
}

func NewRunUsageSummary(sessionID string, startedAt time.Time) *RunUsageSummary {
	return &RunUsageSummary{
		Version:   1,
		Event:     "run_summary",
		SessionID: sessionID,
		StartedAt: startedAt.UTC().Format(time.RFC3339),
	}
}

func ContextWithRunUsageSummary(ctx context.Context, summary *RunUsageSummary) context.Context {
	if summary == nil {
		return ctx
	}
	return context.WithValue(ctx, runUsageSummaryContextKey{}, summary)
}

func RunUsageSummaryFromContext(ctx context.Context) *RunUsageSummary {
	summary, _ := ctx.Value(runUsageSummaryContextKey{}).(*RunUsageSummary)
	return summary
}

func (s *RunUsageSummary) AddUsage(model Model, usage fantasy.Usage, costUSD float64) {
	if s == nil {
		return
	}
	if s.ProviderID == "" {
		s.ProviderID = model.messageProviderID()
	}
	if s.ModelID == "" {
		s.ModelID = model.messageModelID()
	}

	rawInput := int64(usage.InputTokens)
	cacheRead := int64(usage.CacheReadTokens)
	cacheCreation := int64(usage.CacheCreationTokens)
	output := int64(usage.OutputTokens)

	s.RawInputTokens += rawInput
	s.InputCacheHitTokens += cacheRead
	s.InputCacheMissTokens += rawInput
	s.CacheCreationTokens += cacheCreation
	s.CacheReadTokens += cacheRead
	s.OutputTokens += output
	s.ReasoningTokens += int64(usage.ReasoningTokens)
	s.InputTokens = s.RawInputTokens + s.InputCacheHitTokens
	s.TotalTokens = s.InputTokens + s.OutputTokens
	s.CostUSD += costUSD
}

func (s *RunUsageSummary) Finish(finishedAt time.Time) {
	if s == nil {
		return
	}
	s.FinishedAt = finishedAt.UTC().Format(time.RFC3339)
}

func WriteRunUsageSummaryFile(path string, summary *RunUsageSummary) error {
	if path == "" || summary == nil {
		return nil
	}

	data, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("write usage summary: marshal: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".usage-summary-*.tmp")
	if err != nil {
		return fmt.Errorf("write usage summary: create temp: %w", err)
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
		return fmt.Errorf("write usage summary: write temp: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write usage summary: chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write usage summary: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("write usage summary: rename temp: %w", err)
	}
	keep = true
	return nil
}
