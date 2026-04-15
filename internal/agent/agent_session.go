package agent

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/bedrock"
	"charm.land/fantasy/providers/openrouter"
	"charm.land/fantasy/providers/vercel"

	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/taskwarrior"
)

func (a *sessionAgent) Summarize(ctx context.Context, sessionID string, opts fantasy.ProviderOptions) error {
	if a.IsSessionBusy(sessionID) {
		return ErrSessionBusy
	}

	// Copy mutable fields under lock to avoid races with SetModels.
	model := a.model.Get()
	systemPromptPrefix := a.systemPromptPrefix.Get()

	currentSession, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	msgs, err := a.getSessionMessages(ctx, currentSession)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		// Nothing to summarize.
		return nil
	}

	genCtx, cancel := context.WithCancel(ctx)
	a.activeRequests.Set(sessionID, cancel)
	defer a.activeRequests.Del(sessionID)
	defer cancel()

	summaryMessage, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:             message.Assistant,
		Model:            model.Model.Model(),
		Provider:         model.Model.Provider(),
		IsSummaryMessage: true,
	})
	if err != nil {
		return err
	}

	// Build history as text-only fantasy.Messages (no tool-call/result parts).
	history := make([]fantasy.Message, 0, len(msgs))
	for _, m := range msgs {
		history = append(history, m.ToAIMessage()...)
	}

	// Build prompt: system(s) + history + final user prompt.
	prompt := fantasy.Prompt{fantasy.NewSystemMessage(string(summaryPrompt))}
	if systemPromptPrefix != "" {
		prompt = append(prompt, fantasy.NewSystemMessage(systemPromptPrefix))
	}
	prompt = append(prompt, history...)
	prompt = append(prompt, fantasy.NewUserMessage(buildSummaryPrompt(ctx, os.Getenv("TTAL_JOB_ID"))))

	stream, err := model.Model.Stream(genCtx, fantasy.Call{
		Prompt:          prompt,
		ProviderOptions: opts,
		UserAgent:       userAgent,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return a.messages.Delete(ctx, summaryMessage.ID)
		}
		return err
	}

	var totalUsage fantasy.Usage
	var providerMeta fantasy.ProviderMetadata
	for part := range stream {
		switch part.Type {
		case fantasy.StreamPartTypeTextDelta:
			summaryMessage.AppendContent(part.Delta)
			if err := a.messages.Update(genCtx, summaryMessage); err != nil {
				slog.Warn("failed to persist summary text delta", "session_id", sessionID, "err", err)
			}
		case fantasy.StreamPartTypeReasoningDelta:
			summaryMessage.AppendReasoningContent(part.Delta)
			if err := a.messages.Update(genCtx, summaryMessage); err != nil {
				slog.Warn("failed to persist summary reasoning delta", "session_id", sessionID, "err", err)
			}
		case fantasy.StreamPartTypeReasoningEnd:
			if anthropicData, ok := part.ProviderMetadata["anthropic"]; ok {
				if sig, ok := anthropicData.(*anthropic.ReasoningOptionMetadata); ok && sig.Signature != "" {
					summaryMessage.AppendReasoningSignature(sig.Signature)
				}
			}
			summaryMessage.FinishThinking()
			if err := a.messages.Update(genCtx, summaryMessage); err != nil {
				slog.Warn("failed to persist summary reasoning end", "session_id", sessionID, "err", err)
			}
		case fantasy.StreamPartTypeFinish:
			totalUsage = part.Usage
			providerMeta = part.ProviderMetadata
		case fantasy.StreamPartTypeError:
			if errors.Is(part.Error, context.Canceled) {
				return a.messages.Delete(ctx, summaryMessage.ID)
			}
			return part.Error
		}
	}

	summaryMessage.AddFinish(message.FinishReasonEndTurn, "", "")
	if err := a.messages.Update(genCtx, summaryMessage); err != nil {
		return err
	}

	openrouterCost := a.openrouterCost(providerMeta)
	a.updateSessionUsage(model, &currentSession, totalUsage, openrouterCost)
	currentSession.SummaryMessageID = summaryMessage.ID
	currentSession.CompletionTokens = totalUsage.OutputTokens
	currentSession.PromptTokens = 0
	_, err = a.sessions.Save(genCtx, currentSession)
	return err
}

func (a *sessionAgent) getCacheControlOptions() fantasy.ProviderOptions {
	if t, _ := strconv.ParseBool(os.Getenv("LENOS_DISABLE_ANTHROPIC_CACHE")); t {
		return fantasy.ProviderOptions{}
	}
	return fantasy.ProviderOptions{
		anthropic.Name: &anthropic.ProviderCacheControlOptions{
			CacheControl: anthropic.CacheControl{Type: "ephemeral"},
		},
		bedrock.Name: &anthropic.ProviderCacheControlOptions{
			CacheControl: anthropic.CacheControl{Type: "ephemeral"},
		},
		vercel.Name: &anthropic.ProviderCacheControlOptions{
			CacheControl: anthropic.CacheControl{Type: "ephemeral"},
		},
	}
}

func (a *sessionAgent) getSessionMessages(ctx context.Context, s session.Session) ([]message.Message, error) {
	msgs, err := a.messages.List(ctx, s.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	if s.SummaryMessageID != "" {
		summaryMsgIndex := -1
		for i, msg := range msgs {
			if msg.ID == s.SummaryMessageID {
				summaryMsgIndex = i
				break
			}
		}
		if summaryMsgIndex != -1 {
			msgs = msgs[summaryMsgIndex:]
			msgs[0].Role = message.User
		}
	}
	return msgs, nil
}

// generateTitle generates a session titled based on the initial prompt.
func (a *sessionAgent) generateTitle(ctx context.Context, sessionID string, userPrompt string) {
	jobID := os.Getenv("TTAL_JOB_ID")
	var title string
	if jobID == "" {
		slog.Warn("TTAL_JOB_ID not set; using default session name")
		title = DefaultSessionName
	} else {
		cmd := exec.CommandContext(ctx, "task",
			"rc.verbose=nothing", "rc.hooks=off", "rc.confirmation=no", "rc.json.array=on",
			jobID, "export")
		out, err := cmd.Output()
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
				slog.Warn("Task export returned empty array", "jobID", jobID)
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

func (a *sessionAgent) updateSessionUsage(model Model, s *session.Session, usage fantasy.Usage, overrideCost *float64) {
	modelConfig := model.CatwalkCfg
	cost := modelConfig.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
		modelConfig.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
		modelConfig.CostPer1MIn/1e6*float64(usage.InputTokens) +
		modelConfig.CostPer1MOut/1e6*float64(usage.OutputTokens)

	a.eventTokensUsed(s.ID, model, usage, cost)

	if overrideCost != nil {
		s.Cost += *overrideCost
	} else {
		s.Cost += cost
	}

	s.CompletionTokens = usage.OutputTokens
	s.PromptTokens = usage.InputTokens + usage.CacheReadTokens
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

	// Also check for summarize requests.
	if cancel, ok := a.activeRequests.Get(sessionID + "-summarize"); ok && cancel != nil {
		slog.Debug("Summarize cancellation initiated", "session_id", sessionID)
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

func (a *sessionAgent) CancelAll() {
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
	return len(l)
}

func (a *sessionAgent) QueuedPromptsList(sessionID string) []string {
	l, ok := a.messageQueue.Get(sessionID)
	if !ok {
		return nil
	}
	prompts := make([]string, len(l))
	for i, call := range l {
		prompts[i] = call.Prompt
	}
	return prompts
}

func (a *sessionAgent) SetModel(model Model) {
	a.model.Set(model)
}

func (a *sessionAgent) SetTools(tools []fantasy.AgentTool) {
	a.tools.SetSlice(tools)
}

func (a *sessionAgent) SetSystemPrompt(systemPrompt string) {
	a.systemPrompt.Set(systemPrompt)
}

func (a *sessionAgent) Model() Model {
	return a.model.Get()
}

// formatSummaryPrompt formats the session summarization prompt from a todo list.
// Kept separate so benchmarks can test formatting without requiring a context.
func formatSummaryPrompt(todos []session.Todo) string {
	var sb strings.Builder
	sb.WriteString("Provide a detailed summary of our conversation above.")
	if len(todos) > 0 {
		sb.WriteString("\n\n## Current Todo List\n\n")
		for _, t := range todos {
			fmt.Fprintf(&sb, "- [%s] %s\n", t.Status, t.Content)
		}
		sb.WriteString("\nInclude these tasks and their statuses in your summary. ")
		sb.WriteString("Instruct the resuming assistant to use `task <uuid> done` to mark completed subtasks.")
	}
	return sb.String()
}

// buildSummaryPrompt fetches subtasks from taskwarrior and builds the summarization prompt.
func buildSummaryPrompt(ctx context.Context, jobID string) string {
	if jobID == "" {
		return formatSummaryPrompt(nil)
	}
	todos, err := taskwarrior.PollSubtasks(ctx, jobID)
	if err != nil {
		slog.Warn("Failed to poll TW subtasks for summary", "jobID", jobID, "err", err)
		return formatSummaryPrompt(nil)
	}
	return formatSummaryPrompt(todos)
}
