package agent

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openrouter"

	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/session"
)

func (a *sessionAgent) getSessionMessages(ctx context.Context, s session.Session) ([]message.Message, error) {
	msgs, err := a.messages.List(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	// If a compaction boundary exists, only load messages after it.
	// This gives the agent a fresh context window after Compact Session.
	if s.SummaryMessageID != "" {
		found := false
		trimmed := make([]message.Message, 0, len(msgs))
		for _, m := range msgs {
			if m.ID == s.SummaryMessageID {
				found = true
				continue // skip the summary message itself
			}
			if found {
				trimmed = append(trimmed, m)
			}
		}
		if found {
			return trimmed, nil
		}
	}
	return msgs, nil
}

type taskTitleExporter func(context.Context, string) ([]byte, error)

func exportTaskForTitle(ctx context.Context, taskID string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "task",
		"rc.verbose=nothing", "rc.hooks=off", "rc.confirmation=no", "rc.json.array=on",
		taskID, "export")
	return cmd.Output()
}

// generateTitle refreshes the session title from the current task.
func (a *sessionAgent) generateTitle(ctx context.Context, sessionID, taskID string) {
	var title string
	if taskID == "" {
		return
	} else {
		exporter := a.taskExporter
		if exporter == nil {
			exporter = exportTaskForTitle
		}
		out, err := exporter(ctx, taskID)
		if err != nil {
			slog.Warn("Failed to export task for title", "err", err)
			title = DefaultSessionName
		} else {
			var tasks []struct {
				Description string `json:"description"`
			}
			if err := json.Unmarshal(out, &tasks); err != nil {
				slog.Warn("Failed to parse task export JSON", "err", err)
				title = DefaultSessionName
			} else if len(tasks) == 0 {
				slog.Warn("Task export returned empty array", "taskID", taskID)
				title = DefaultSessionName
			} else {
				title = strings.TrimSpace(tasks[0].Description)
				if len(title) > 100 {
					title = title[:100]
				}
				title = cmp.Or(title, DefaultSessionName)
			}
		}
	}

	a.sessionUpdateMu.Lock()
	defer a.sessionUpdateMu.Unlock()
	if err := a.sessions.Rename(ctx, sessionID, title); err != nil {
		slog.Error("Failed to save session title", "error", err)
	}
}

func (a *sessionAgent) openrouterCost(metadata fantasy.ProviderMetadata) *float64 {
	openrouterMetadata, ok := metadata[openrouter.Name]
	if !ok {
		return nil
	}

	opts, ok := openrouterMetadata.(*openrouter.ProviderMetadata)
	if !ok {
		return nil
	}
	return &opts.Usage.Cost
}

func usageCost(model Model, usage fantasy.Usage, overrideCost *float64) float64 {
	if overrideCost != nil {
		return *overrideCost
	}

	modelConfig := model.CatwalkCfg
	return modelConfig.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
		modelConfig.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
		modelConfig.CostPer1MIn/1e6*float64(usage.InputTokens) +
		modelConfig.CostPer1MOut/1e6*float64(usage.OutputTokens)
}

func (a *sessionAgent) updateSessionUsage(model Model, s *session.Session, usage fantasy.Usage, overrideCost *float64) {
	cost := usageCost(model, usage, overrideCost)

	a.eventTokensUsed(s.ID, model, usage, cost)

	s.Cost += cost

	if usage.InputTokens+usage.OutputTokens+usage.CacheCreationTokens+usage.CacheReadTokens == 0 {
		return
	}

	s.CompletionTokens = usage.OutputTokens
	s.PromptTokens = usage.InputTokens + usage.CacheReadTokens
	s.CacheCreationTokens += usage.CacheCreationTokens
	s.CacheReadTokens += usage.CacheReadTokens
	s.CacheMissTokens += usage.InputTokens + usage.CacheCreationTokens
}

// saveSessionUsage fetches the session, updates usage metrics with the supplied
// per-turn totals, saves it, and returns the updated session. On any error the
// original session is returned unchanged and a warning is logged.
func (a *sessionAgent) saveSessionUsage(ctx context.Context, sessionID string, usage fantasy.Usage, meta fantasy.ProviderMetadata, logMsg string) (session.Session, bool) {
	pm := a.primaryModel.Get()
	a.sessionUpdateMu.Lock()
	defer a.sessionUpdateMu.Unlock()
	s, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		slog.Warn("Failed to load session for usage update", "session_id", sessionID, "error", err)
		return session.Session{}, false
	}
	a.updateSessionUsage(pm, &s, usage, a.openrouterCost(meta))
	updated, saveErr := a.sessions.Save(ctx, s)
	if saveErr != nil {
		slog.Warn(logMsg, "session_id", sessionID, "error", saveErr)
		return s, false
	}
	return updated, true
}

func (a *sessionAgent) Cancel(sessionID string) {
	// Cancel regular requests. Don't use Take() here - we need the entry to
	// remain in activeRequests so IsBusy() returns true until the goroutine
	// fully completes (including error handling that may access the DB).
	// The defer in processRequest will clean up the entry.
	if cancel, ok := a.activeRequests.Get(sessionID); ok && cancel != nil {
		slog.Debug("Request cancellation initiated", "session_id", sessionID)
		cancel()
	}

	if a.QueuedPrompts(sessionID) > 0 {
		slog.Debug("Clearing queued prompts", "session_id", sessionID)
		a.messageQueue.Del(sessionID)
	}
}

func (a *sessionAgent) ClearQueue(sessionID string) {
	if a.QueuedPrompts(sessionID) > 0 {
		slog.Debug("Clearing queued prompts", "session_id", sessionID)
		a.messageQueue.Del(sessionID)
	}
}

func (a *sessionAgent) ActiveBackgroundJobs(sessionID string) []BackgroundJob {
	a.bgRunnersMu.Lock()
	br, ok := a.bgRunners.Get(sessionID)
	a.bgRunnersMu.Unlock()
	if !ok || br == nil {
		return nil
	}
	return br.ListActive()
}

func (a *sessionAgent) KillBackgroundJob(ctx context.Context, sessionID, jobID string) error {
	a.bgRunnersMu.Lock()
	br, ok := a.bgRunners.Get(sessionID)
	a.bgRunnersMu.Unlock()
	if !ok || br == nil {
		return fmt.Errorf("no background jobs for session %s", sessionID)
	}
	return br.KillJob(jobID)
}

func (a *sessionAgent) StopBackgroundJobs(sessionID string) {
	a.bgRunnersMu.Lock()
	br, ok := a.bgRunners.Get(sessionID)
	if ok && br != nil {
		br.StopAll()
		a.bgRunners.Del(sessionID)
	}
	a.bgRunnersMu.Unlock()
}

func (a *sessionAgent) CancelAll() {
	// Cancel running background jobs.
	a.bgRunnersMu.Lock()
	for sessionID, br := range a.bgRunners.Seq2() {
		br.StopAll()
		a.bgRunners.Del(sessionID)
	}
	a.bgRunnersMu.Unlock()

	if !a.IsBusy() {
		return
	}
	for key := range a.activeRequests.Seq2() {
		a.Cancel(key) // key is sessionID
	}

	timeout := time.After(5 * time.Second)
	for a.IsBusy() {
		select {
		case <-timeout:
			return
		default:
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func (a *sessionAgent) IsBusy() bool {
	var busy bool
	for cancelFunc := range a.activeRequests.Seq() {
		if cancelFunc != nil {
			busy = true
			break
		}
	}
	return busy
}

func (a *sessionAgent) IsSessionBusy(sessionID string) bool {
	_, busy := a.activeRequests.Get(sessionID)
	return busy
}

func (a *sessionAgent) QueuedPrompts(sessionID string) int {
	l, ok := a.messageQueue.Get(sessionID)
	if !ok {
		return 0
	}
	count := 0
	for _, call := range l {
		if call.runtimePrompt {
			continue
		}
		count++
	}
	return count
}

func (a *sessionAgent) QueuedPromptsList(sessionID string) []string {
	l, ok := a.messageQueue.Get(sessionID)
	if !ok {
		return nil
	}
	prompts := make([]string, 0, len(l))
	for _, call := range l {
		if call.runtimePrompt {
			continue
		}
		prompts = append(prompts, call.Prompt)
	}
	return prompts
}

func (a *sessionAgent) SetModels(large Model, small Model, primary Model) {
	a.largeModel.Set(large)
	a.smallModel.Set(small)
	a.primaryModel.Set(primary)
}

func (a *sessionAgent) SetSystemPrompt(systemPrompt string) {
	a.systemPrompt.Set(systemPrompt)
}

func (a *sessionAgent) Model() Model {
	return a.primaryModel.Get()
}

func (a *sessionAgent) CompactSession(ctx context.Context, call SessionAgentCall) error {
	call.MarkCompactBoundary = true
	return a.Run(ctx, call)
}
