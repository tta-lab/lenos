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

	"github.com/tta-lab/lenos/internal/agent/lenosbash"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/taskwarrior"
)

func (a *sessionAgent) Summarize(ctx context.Context, sessionID string, opts fantasy.ProviderOptions) error {
	if a.IsSessionBusy(sessionID) {
		return ErrSessionBusy
	}

	// Copy mutable fields under lock to avoid races with SetModels.
	summaryModel := a.primaryModel.Get()
	systemPrompt := a.systemPrompt.Get()

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
		Model:            summaryModel.messageModelID(),
		Provider:         summaryModel.messageProviderID(),
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

	// Build prompt: normal system prompt + history + final compact request.
	prompt := fantasy.Prompt{fantasy.NewSystemMessage(systemPrompt)}
	prompt = append(prompt, history...)
	prompt = append(prompt, fantasy.NewUserMessage(buildCompactSummaryPrompt(ctx, taskwarrior.ResolveTaskIDFromCwd())))

	baseline := summaryMessage.Clone()
	streamResult, err := retryModelStream(genCtx,
		func() (summaryStreamResult, error) {
			return streamSummaryAttempt(genCtx, summaryModel.Model, prompt, opts, a.messages, &summaryMessage)
		},
		func() {
			resetMessageForStreamRetry(genCtx, a.messages, &summaryMessage, baseline, "summary: reset message for stream retry")
		},
	)
	if err != nil {
		deleteErr := a.messages.Delete(ctx, summaryMessage.ID)
		if errors.Is(err, context.Canceled) {
			return deleteErr
		}
		return errors.Join(err, deleteErr)
	}

	totalUsage := streamResult.usage
	providerMeta := streamResult.meta
	summaryMessage = streamResult.message
	normalizeSummaryMarkdown(&summaryMessage)

	summaryMessage.AddFinish(message.FinishReasonEndTurn, "", "")
	if err := a.messages.Update(genCtx, summaryMessage); err != nil {
		return err
	}

	openrouterCost := a.openrouterCost(providerMeta)
	a.updateSessionUsage(summaryModel, &currentSession, totalUsage, openrouterCost)
	currentSession.SummaryMessageID = summaryMessage.ID
	currentSession.CompletionTokens = totalUsage.OutputTokens
	currentSession.PromptTokens = 0
	_, err = a.sessions.Save(genCtx, currentSession)
	return err
}

type summaryStreamResult struct {
	message message.Message
	usage   fantasy.Usage
	meta    fantasy.ProviderMetadata
}

func streamSummaryAttempt(
	ctx context.Context,
	model fantasy.LanguageModel,
	prompt fantasy.Prompt,
	opts fantasy.ProviderOptions,
	messages message.Service,
	summaryMessage *message.Message,
) (summaryStreamResult, error) {
	stream, err := model.Stream(ctx, fantasy.Call{
		Prompt:          prompt,
		ProviderOptions: opts,
		UserAgent:       userAgent,
	})
	if err != nil {
		return summaryStreamResult{message: *summaryMessage}, err
	}

	var totalUsage fantasy.Usage
	var providerMeta fantasy.ProviderMetadata
	for part := range stream {
		switch part.Type {
		case fantasy.StreamPartTypeTextDelta:
			summaryMessage.AppendContent(part.Delta)
			if err := messages.Update(ctx, *summaryMessage); err != nil {
				slog.Warn("failed to persist summary text delta", "err", err)
			}
		case fantasy.StreamPartTypeReasoningDelta:
			summaryMessage.AppendReasoningContent(part.Delta)
			if err := messages.Update(ctx, *summaryMessage); err != nil {
				slog.Warn("failed to persist summary reasoning delta", "err", err)
			}
		case fantasy.StreamPartTypeReasoningEnd:
			if anthropicData, ok := part.ProviderMetadata["anthropic"]; ok {
				if sig, ok := anthropicData.(*anthropic.ReasoningOptionMetadata); ok && sig.Signature != "" {
					summaryMessage.AppendReasoningSignature(sig.Signature)
				}
			}
			summaryMessage.FinishThinking()
			if err := messages.Update(ctx, *summaryMessage); err != nil {
				slog.Warn("failed to persist summary reasoning end", "err", err)
			}
		case fantasy.StreamPartTypeFinish:
			totalUsage = part.Usage
			providerMeta = part.ProviderMetadata
		case fantasy.StreamPartTypeError:
			return summaryStreamResult{message: *summaryMessage, usage: totalUsage, meta: providerMeta}, part.Error
		}
	}

	return summaryStreamResult{message: *summaryMessage, usage: totalUsage, meta: providerMeta}, nil
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
			summaryMsg := msgs[summaryMsgIndex]
			summaryMsg.Role = message.User

			compacted := make([]message.Message, 0, 1+recentUserMessagesAfterCompact+len(msgs[summaryMsgIndex+1:]))
			compacted = append(compacted, summaryMsg)
			compacted = append(compacted, recentUserMessages(msgs[:summaryMsgIndex], recentUserMessagesAfterCompact)...)
			compacted = append(compacted, msgs[summaryMsgIndex+1:]...)
			msgs = compacted
		}
	}
	return msgs, nil
}

func recentUserMessages(msgs []message.Message, limit int) []message.Message {
	if limit <= 0 {
		return nil
	}
	recent := make([]message.Message, 0, limit)
	for i := len(msgs) - 1; i >= 0 && len(recent) < limit; i-- {
		if msgs[i].Role != message.User {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(msgs[i].Content().Text), autoCompactContinuationPrefix) {
			continue
		}
		recent = append(recent, msgs[i])
	}
	for i, j := 0, len(recent)-1; i < j; i, j = i+1, j-1 {
		recent[i], recent[j] = recent[j], recent[i]
	}
	return recent
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

// saveSessionUsage fetches the session, updates usage metrics with the supplied
// per-turn totals, saves it, and returns the updated session. On any error the
// original session is returned unchanged and a warning is logged.
func (a *sessionAgent) saveSessionUsage(ctx context.Context, sessionID string, usage fantasy.Usage, meta fantasy.ProviderMetadata, logMsg string) (session.Session, bool) {
	pm := a.primaryModel.Get()
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

func (a *sessionAgent) ActiveBackgroundJobs(sessionID string) []BackgroundJob {
	watcher, ok := a.jobWatchers.Get(sessionID)
	if !ok || watcher == nil {
		return nil
	}
	return watcher.ListActive()
}

func (a *sessionAgent) KillBackgroundJob(ctx context.Context, sessionID, jobID string) error {
	watcher, ok := a.jobWatchers.Get(sessionID)
	if !ok || watcher == nil {
		return fmt.Errorf("no background jobs for session %s", sessionID)
	}
	return watcher.KillJob(ctx, jobID)
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

func summaryInstructionsPrompt() string {
	return strings.TrimRight(string(summaryPrompt), "\n")
}

func summaryOutputProtocolPrompt() string {
	return `Output protocol:

Emit only the summary Markdown. Do not emit Markdown fences, JSON, XML,
comments, or any text before or after the summary.
`
}

func normalizeSummaryMarkdown(summaryMessage *message.Message) {
	parsed, diag := lenosbash.Parse(summaryMessage.Content().Text)
	if diag != nil || len(parsed.Bash) > 0 || strings.TrimSpace(parsed.Prose) == "" {
		return
	}
	replaceAssistantText(summaryMessage, parsed.Prose)
}

func buildCompactSummaryPrompt(ctx context.Context, jobID string) string {
	return strings.Join([]string{
		summaryInstructionsPrompt(),
		buildSummaryPrompt(ctx, jobID),
		summaryOutputProtocolPrompt(),
	}, "\n\n")
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
