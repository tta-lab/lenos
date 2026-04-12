// Package agent is the core orchestration layer for Lenos AI agents.
//
// It provides session-based AI agent functionality for managing
// conversations, tool execution, and message handling. It coordinates
// interactions between language models, messages, sessions, and tools while
// handling features like automatic summarization, queuing, and token
// management.
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
	"sync"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/bedrock"
	"charm.land/fantasy/providers/openrouter"
	"charm.land/fantasy/providers/vercel"
	"charm.land/lipgloss/v2"

	"github.com/tta-lab/lenos/internal/agent/hyper"
	"github.com/tta-lab/lenos/internal/stringext"
	"github.com/tta-lab/logos"

	"github.com/tta-lab/lenos/internal/agent/notify"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/csync"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/pubsub"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/taskwarrior"
	"github.com/tta-lab/lenos/internal/version"
)

const (
	DefaultSessionName = "Untitled Session"

	// Constants for auto-summarization thresholds
	largeContextWindowThreshold = 200_000
	largeContextWindowBuffer    = 20_000
	smallContextWindowRatio     = 0.2
)

var userAgent = fmt.Sprintf("Lenos/%s (https://github.com/tta-lab/lenos)", version.Version)

//go:embed templates/summary.md
var summaryPrompt []byte

type SessionAgentCall struct {
	SessionID string
	Prompt    string
	LogosCfg  logos.Config
}

type SessionAgent interface {
	Run(context.Context, SessionAgentCall) (*logos.RunResult, error)
	SetModels(large Model, small Model)
	SetTools(tools []fantasy.AgentTool)
	SetSystemPrompt(systemPrompt string)
	Cancel(sessionID string)
	CancelAll()
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	QueuedPrompts(sessionID string) int
	QueuedPromptsList(sessionID string) []string
	ClearQueue(sessionID string)
	Summarize(context.Context, string, fantasy.ProviderOptions) error
	Model() Model
}

type Model struct {
	Model      fantasy.LanguageModel
	CatwalkCfg catwalk.Model
	ModelCfg   config.SelectedModel
}

type sessionAgent struct {
	largeModel         *csync.Value[Model]
	smallModel         *csync.Value[Model]
	systemPromptPrefix *csync.Value[string]
	systemPrompt       *csync.Value[string]
	tools              *csync.Slice[fantasy.AgentTool]

	isSubAgent           bool
	sessions             session.Service
	messages             message.Service
	disableAutoSummarize bool
	notify               pubsub.Publisher[notify.Notification]

	messageQueue   *csync.Map[string, []SessionAgentCall]
	activeRequests *csync.Map[string, context.CancelFunc]
}

type SessionAgentOptions struct {
	LargeModel           Model
	SmallModel           Model
	SystemPromptPrefix   string
	SystemPrompt         string
	IsSubAgent           bool
	DisableAutoSummarize bool
	Sessions             session.Service
	Messages             message.Service
	Tools                []fantasy.AgentTool
	Notify               pubsub.Publisher[notify.Notification]
}

func NewSessionAgent(
	opts SessionAgentOptions,
) SessionAgent {
	return &sessionAgent{
		largeModel:           csync.NewValue(opts.LargeModel),
		smallModel:           csync.NewValue(opts.SmallModel),
		systemPromptPrefix:   csync.NewValue(opts.SystemPromptPrefix),
		systemPrompt:         csync.NewValue(opts.SystemPrompt),
		isSubAgent:           opts.IsSubAgent,
		sessions:             opts.Sessions,
		messages:             opts.Messages,
		disableAutoSummarize: opts.DisableAutoSummarize,
		tools:                csync.NewSliceFrom(opts.Tools),
		notify:               opts.Notify,
		messageQueue:         csync.NewMap[string, []SessionAgentCall](),
		activeRequests:       csync.NewMap[string, context.CancelFunc](),
	}
}

func (a *sessionAgent) Run(ctx context.Context, call SessionAgentCall) (*logos.RunResult, error) {
	var currentAssistant *message.Message
	var createdAssistantMsgs []*message.Message
	var turnJustEnded bool
	var pendingCommands map[string][]string

runLoop:
	// Reset closure state to prevent stale-message misalignment on queued turns.
	currentAssistant = nil
	createdAssistantMsgs = nil
	turnJustEnded = false
	pendingCommands = make(map[string][]string)

	if call.Prompt == "" {
		return nil, ErrEmptyPrompt
	}
	if call.SessionID == "" {
		return nil, ErrSessionMissing
	}

	// Queue the message if busy
	if a.IsSessionBusy(call.SessionID) {
		existing, ok := a.messageQueue.Get(call.SessionID)
		if !ok {
			existing = []SessionAgentCall{}
		}
		existing = append(existing, call)
		a.messageQueue.Set(call.SessionID, existing)
		return nil, nil
	}

	currentSession, err := a.sessions.Get(ctx, call.SessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	msgs, err := a.getSessionMessages(ctx, currentSession)
	if err != nil {
		return nil, fmt.Errorf("failed to get session messages: %w", err)
	}

	// Persist the user message to the database so it appears in the session history.
	if _, err := a.messages.Create(ctx, call.SessionID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: call.Prompt}},
	}); err != nil {
		return nil, fmt.Errorf("failed to create user message: %w", err)
	}

	var wg sync.WaitGroup
	// Generate title if first message.
	if len(msgs) == 0 {
		titleCtx := ctx
		wg.Go(func() {
			a.generateTitle(titleCtx, call.SessionID, call.Prompt)
		})
	}
	defer wg.Wait()

	streamCtx, cancel := context.WithCancel(ctx)
	a.activeRequests.Set(call.SessionID, cancel)
	defer cancel()
	defer a.activeRequests.Del(call.SessionID)

	history := make([]fantasy.Message, 0, len(msgs))
	for _, m := range msgs {
		history = append(history, m.ToAIMessage()...)
	}
	// Add the user message to history so the LLM sees the full context.
	history = append(history, fantasy.NewUserMessage(call.Prompt))

	startTime := time.Now()
	a.eventPromptSent(call.SessionID)

	callbacks := logos.Callbacks{
		OnDelta: func(text string) {
			// Detect <cmd> block: logos emits the full <cmd>...</cmd> text as one chunk.
			if strings.HasPrefix(text, "<cmd>") {
				cmdText, _, _ := strings.Cut(text, "</cmd>")
				cmdText = strings.TrimPrefix(cmdText, "<cmd>")
				cmdText = strings.TrimSpace(cmdText)
				if cmdText == "" {
					return
				}
				// Emit pending command to UI (shows "running..." before result arrives).
				msg, err := a.messages.Create(ctx, call.SessionID, message.CreateMessageParams{
					Role:  message.Result,
					Parts: []message.ContentPart{message.CommandContent{Command: cmdText, Pending: true}},
				})
				if err != nil {
					slog.Warn("Failed to create pending command message", "error", err)
					// Fall through: let OnCommandResult create the completed message directly.
				} else {
					pendingCommands[cmdText] = append(pendingCommands[cmdText], msg.ID)
				}
				return
			}

			if turnJustEnded {
				currentAssistant = nil
				turnJustEnded = false
			}
			if currentAssistant == nil {
				text = strings.TrimPrefix(text, "\n")
				msg, err := a.messages.Create(ctx, call.SessionID, message.CreateMessageParams{
					Role:     message.Assistant,
					Parts:    []message.ContentPart{},
					Model:    call.LogosCfg.Model,
					Provider: call.LogosCfg.Provider.Name(),
				})
				if err != nil {
					slog.Warn("Failed to create assistant message", "error", err)
					return
				}
				currentAssistant = &msg
				createdAssistantMsgs = append(createdAssistantMsgs, currentAssistant)
			}
			currentAssistant.AppendContent(text)
			if err := a.messages.Update(ctx, *currentAssistant); err != nil {
				slog.Warn("Failed to update assistant message", "error", err)
			}
		},
		OnCommandResult: func(command string, output string, exitCode int) {
			// FIFO: pop the first message ID for this command.
			if msgIDs, ok := pendingCommands[command]; ok && len(msgIDs) > 0 {
				msgID := msgIDs[0]
				pendingCommands[command] = msgIDs[1:]
				if len(pendingCommands[command]) == 0 {
					delete(pendingCommands, command)
				}
				existing, err := a.messages.Get(ctx, msgID)
				if err != nil {
					// Get failed — fall through to create a new result message.
					goto createCommandResult
				}
				existing.Parts = []message.ContentPart{message.CommandContent{Command: command, Output: output, ExitCode: &exitCode, Pending: false}}
				if err := a.messages.Update(ctx, existing); err != nil {
					slog.Warn("Failed to update pending command message", "error", err)
					// Fall through to create a new message so output is not lost.
					goto createCommandResult
				}
				return
			createCommandResult:
			}
			// Fallback: create a new result message for the command.
			_, err := a.messages.Create(ctx, call.SessionID, message.CreateMessageParams{
				Role:  message.Result,
				Parts: []message.ContentPart{message.CommandContent{Command: command, Output: output, ExitCode: &exitCode, Pending: false}},
			})
			if err != nil {
				slog.Warn("Failed to persist command result", "error", err)
			}
			turnJustEnded = true
		},
		OnRetry: func(reason string, step int) {
			slog.Warn("Logos retry", "reason", reason, "step", step)
		},
	}

	result, runErr := logos.Run(streamCtx, call.LogosCfg, history, call.Prompt, callbacks)

	a.eventPromptResponded(call.SessionID, time.Since(startTime).Truncate(time.Second))

	a.backfillReasoning(ctx, result, createdAssistantMsgs)

	if runErr != nil {
		isCancelErr := errors.Is(runErr, context.Canceled)
		if currentAssistant != nil {
			currentAssistant.FinishThinking()
			if isCancelErr {
				currentAssistant.AddFinish(message.FinishReasonCanceled, "User canceled request", "")
			} else {
				var fantasyErr *fantasy.Error
				var providerErr *fantasy.ProviderError
				const defaultTitle = "Provider Error"
				linkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6b8a5e")).Underline(true)
				if errors.Is(runErr, hyper.ErrNoCredits) {
					url := hyper.BaseURL()
					link := linkStyle.Hyperlink(url, "id=hyper").Render(url)
					currentAssistant.AddFinish(message.FinishReasonError, "No credits", "You're out of credits. Add more at "+link)
				} else if errors.As(runErr, &providerErr) {
					if providerErr.Message == "The requested model is not supported." {
						url := "https://github.com/settings/copilot/features"
						link := linkStyle.Hyperlink(url, "id=copilot").Render(url)
						currentAssistant.AddFinish(
							message.FinishReasonError,
							"Copilot model not enabled",
							fmt.Sprintf("%q is not enabled in Copilot. Go to the following page to enable it. Then, wait 5 minutes before trying again. %s", call.LogosCfg.Model, link),
						)
					} else {
						currentAssistant.AddFinish(message.FinishReasonError, cmp.Or(stringext.Capitalize(providerErr.Title), defaultTitle), providerErr.Message)
					}
				} else if errors.As(runErr, &fantasyErr) {
					currentAssistant.AddFinish(message.FinishReasonError, cmp.Or(stringext.Capitalize(fantasyErr.Title), defaultTitle), fantasyErr.Message)
				} else {
					currentAssistant.AddFinish(message.FinishReasonError, defaultTitle, runErr.Error())
				}
			}
			if updateErr := a.messages.Update(ctx, *currentAssistant); updateErr != nil {
				slog.Warn("Failed to update assistant message on error", "error", updateErr)
			}
		}
		// Queue next message before returning (non-recursive drain).
		a.activeRequests.Del(call.SessionID)
		cancel()

		queuedMessages, ok := a.messageQueue.Get(call.SessionID)
		if ok && len(queuedMessages) > 0 {
			nextCall := queuedMessages[0]
			a.messageQueue.Set(call.SessionID, queuedMessages[1:])
			call = nextCall
			goto runLoop
		}
		return nil, runErr
	}

	// Send notification that agent has finished its turn.
	if a.notify != nil {
		a.notify.Publish(pubsub.CreatedEvent, notify.Notification{
			SessionID:    call.SessionID,
			SessionTitle: currentSession.Title,
			Type:         notify.TypeAgentFinished,
		})
	}

	// Queue next message before returning (non-recursive drain).
	a.activeRequests.Del(call.SessionID)
	cancel()

	queuedMessages, ok := a.messageQueue.Get(call.SessionID)
	if !ok || len(queuedMessages) == 0 {
		return result, nil
	}
	nextCall := queuedMessages[0]
	a.messageQueue.Set(call.SessionID, queuedMessages[1:])
	call = nextCall
	goto runLoop
}

func (a *sessionAgent) backfillReasoning(ctx context.Context, result *logos.RunResult, createdAssistantMsgs []*message.Message) {
	if result == nil {
		return
	}
	var assistantSteps []logos.StepMessage
	for _, s := range result.Steps {
		if s.Role == logos.StepRoleAssistant {
			assistantSteps = append(assistantSteps, s)
		}
	}
	n := min(len(assistantSteps), len(createdAssistantMsgs))
	for i := 0; i < n; i++ {
		s := assistantSteps[i]
		if s.Reasoning == "" && s.ReasoningSignature == "" {
			continue
		}
		msg := createdAssistantMsgs[i]
		if s.Reasoning != "" {
			msg.AppendReasoningContent(s.Reasoning)
		}
		if s.ReasoningSignature != "" {
			msg.AppendReasoningSignature(s.ReasoningSignature)
		}
		if err := a.messages.Update(ctx, *msg); err != nil {
			slog.Warn("reasoning backfill failed", "msg_id", msg.ID, "error", err)
		}
	}
	if len(assistantSteps) != len(createdAssistantMsgs) {
		slog.Warn("reasoning backfill: step count mismatch",
			"assistant_steps", len(assistantSteps), "created_msgs", len(createdAssistantMsgs))
	}
}

func (a *sessionAgent) Summarize(ctx context.Context, sessionID string, opts fantasy.ProviderOptions) error {
	if a.IsSessionBusy(sessionID) {
		return ErrSessionBusy
	}

	// Copy mutable fields under lock to avoid races with SetModels.
	largeModel := a.largeModel.Get()
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
		Model:            largeModel.Model.Model(),
		Provider:         largeModel.Model.Provider(),
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

	stream, err := largeModel.Model.Stream(genCtx, fantasy.Call{
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
	a.updateSessionUsage(largeModel, &currentSession, totalUsage, openrouterCost)
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

func (a *sessionAgent) getSessionMessages(ctx context.Context, session session.Session) ([]message.Message, error) {
	msgs, err := a.messages.List(ctx, session.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	if session.SummaryMessageID != "" {
		summaryMsgIndex := -1
		for i, msg := range msgs {
			if msg.ID == session.SummaryMessageID {
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

func (a *sessionAgent) updateSessionUsage(model Model, session *session.Session, usage fantasy.Usage, overrideCost *float64) {
	modelConfig := model.CatwalkCfg
	cost := modelConfig.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
		modelConfig.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
		modelConfig.CostPer1MIn/1e6*float64(usage.InputTokens) +
		modelConfig.CostPer1MOut/1e6*float64(usage.OutputTokens)

	a.eventTokensUsed(session.ID, model, usage, cost)

	if overrideCost != nil {
		session.Cost += *overrideCost
	} else {
		session.Cost += cost
	}

	session.CompletionTokens = usage.OutputTokens
	session.PromptTokens = usage.InputTokens + usage.CacheReadTokens
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

func (a *sessionAgent) SetModels(large Model, small Model) {
	a.largeModel.Set(large)
	a.smallModel.Set(small)
}

func (a *sessionAgent) SetTools(tools []fantasy.AgentTool) {
	a.tools.SetSlice(tools)
}

func (a *sessionAgent) SetSystemPrompt(systemPrompt string) {
	a.systemPrompt.Set(systemPrompt)
}

func (a *sessionAgent) Model() Model {
	return a.largeModel.Get()
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
