package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"charm.land/fantasy"
	"charm.land/lipgloss/v2"

	"github.com/tta-lab/lenos/internal/agent/hyper"
	"github.com/tta-lab/lenos/internal/agent/notify"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/pubsub"
	"github.com/tta-lab/lenos/internal/stringext"
	"github.com/tta-lab/logos/v2"
)

// runState holds mutable state for the duration of a single Run() call.
// Each queued turn reinitializes these fields via the runLoop reset block.
type runState struct {
	sessionID  string
	providerID string // config provider ID (e.g. "minimax-china"), NOT fantasy protocol name
	logosCfg   logos.Config
	messages   message.Service
	ctx        context.Context

	currentAssistant *message.Message
	pendingResult    *message.Message
}

// buildHistory converts session messages to fantasy messages for logos.Run.
// The prompt is NOT included — logos.Run appends it internally.
func buildHistory(msgs []message.Message) []fantasy.Message {
	history := make([]fantasy.Message, 0, len(msgs))
	for _, m := range msgs {
		history = append(history, m.ToAIMessage()...)
	}
	return history
}

func (state *runState) handleStepStart(_ int) {
	state.currentAssistant = nil
	if state.providerID == "" {
		slog.Error("handleStepStart: providerID is empty — UI model lookups will fail")
	}
	msg, err := state.messages.Create(state.ctx, state.sessionID, message.CreateMessageParams{
		Role:     message.Assistant,
		Parts:    []message.ContentPart{message.TextContent{Text: ""}},
		Model:    state.logosCfg.Model,
		Provider: state.providerID,
	})
	if err != nil {
		slog.Warn("handleStepStart: failed to create assistant message", "error", err)
		return
	}
	state.currentAssistant = &msg
}

// extractCmdText parses the command text from a <cmd>...</cmd> chunk.
// Returns the empty string if the block is empty or malformed.
func extractCmdText(chunk string) string {
	cmdText, _, _ := strings.Cut(chunk, "</cmd>")
	cmdText = strings.TrimPrefix(cmdText, "<cmd>")
	return strings.TrimSpace(cmdText)
}

// errorFinishFor returns an appropriate FinishReason and user-facing message for a
// logos.Run error. This provides actionable feedback (e.g. "enable Copilot model",
// "add credits") rather than opaque error strings.
func errorFinishFor(runErr error, model string) (reason message.FinishReason, title, msg string) {
	reason = message.FinishReasonError
	const defaultTitle = "Provider Error"
	linkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6b8a5e")).Underline(true)

	if errors.Is(runErr, hyper.ErrNoCredits) {
		url := hyper.BaseURL()
		link := linkStyle.Hyperlink(url, "id=hyper").Render(url)
		return reason, "No credits", "You're out of credits. Add more at " + link
	}

	var fantasyErr *fantasy.Error
	var providerErr *fantasy.ProviderError
	if errors.As(runErr, &providerErr) {
		if providerErr.Message == "The requested model is not supported." {
			url := "https://github.com/settings/copilot/features"
			link := linkStyle.Hyperlink(url, "id=copilot").Render(url)
			return reason, "Copilot model not enabled",
				fmt.Sprintf("%q is not enabled in Copilot. Go to the following page to enable it. Then, wait 5 minutes before trying again. %s", model, link)
		}
		return reason, cmp.Or(stringext.Capitalize(providerErr.Title), defaultTitle), providerErr.Message
	}
	if errors.As(runErr, &fantasyErr) {
		return reason, cmp.Or(stringext.Capitalize(fantasyErr.Title), defaultTitle), fantasyErr.Message
	}
	return reason, defaultTitle, runErr.Error()
}

func (state *runState) handleStepEnd(_ int) {
	if state.currentAssistant == nil {
		return
	}
	if !state.currentAssistant.IsFinished() {
		state.currentAssistant.AddFinish(message.FinishReasonToolUse, "", "")
		if err := state.messages.Update(state.ctx, *state.currentAssistant); err != nil {
			slog.Warn("handleStepEnd: failed to persist finish", "error", err)
		}
	}
}

func (state *runState) handleDelta(text string) {
	if state.currentAssistant == nil {
		slog.Warn("handleDelta: no currentAssistant, creating placeholder", "text_prefix", text[:min(len(text), 30)])
		state.handleStepStart(0)
		if state.currentAssistant == nil {
			return
		}
	}

	if strings.HasPrefix(text, "<cmd>") {
		cmdText := extractCmdText(text)
		if cmdText == "" {
			return
		}
		state.currentAssistant.AppendContent(text)
		if err := state.messages.Update(state.ctx, *state.currentAssistant); err != nil {
			slog.Warn("handleDelta: failed to persist cmd content", "error", err)
		}
		msg, err := state.messages.Create(state.ctx, state.sessionID, message.CreateMessageParams{
			Role:  message.Result,
			Parts: []message.ContentPart{message.CommandContent{Command: cmdText, Pending: true}},
		})
		if err != nil {
			slog.Warn("handleDelta: failed to create pending command message", "error", err)
		} else {
			state.pendingResult = &msg
		}
		return
	}

	// Prose delta: check if transitioning from reasoning to prose.
	if state.currentAssistant.ReasoningContent().Thinking != "" && state.currentAssistant.ReasoningContent().FinishedAt == 0 {
		state.currentAssistant.FinishThinking()
		if err := state.messages.Update(state.ctx, *state.currentAssistant); err != nil {
			slog.Warn("handleDelta: failed to persist reasoning finish", "error", err)
		}
	}
	state.currentAssistant.AppendContent(text)
	if err := state.messages.Update(state.ctx, *state.currentAssistant); err != nil {
		slog.Warn("handleDelta: failed to persist content", "error", err)
	}
}

func (state *runState) handleReasoningDelta(text string) {
	if state.currentAssistant == nil {
		slog.Warn("handleReasoningDelta: no currentAssistant", "text_prefix", text[:min(len(text), 30)])
		state.handleStepStart(0)
		if state.currentAssistant == nil {
			return
		}
	}
	state.currentAssistant.AppendReasoningContent(text)
	if err := state.messages.Update(state.ctx, *state.currentAssistant); err != nil {
		slog.Warn("handleReasoningDelta: failed to persist reasoning", "error", err)
	}
}

func (state *runState) handleReasoningSignature(sig string) {
	if state.currentAssistant == nil {
		return
	}
	state.currentAssistant.AppendReasoningSignature(sig)
	state.currentAssistant.FinishThinking()
	if err := state.messages.Update(state.ctx, *state.currentAssistant); err != nil {
		slog.Warn("handleReasoningSignature: failed to persist", "error", err)
	}
}

func (state *runState) handleCommandResult(command string, output string, exitCode int) {
	if state.pendingResult != nil {
		state.pendingResult.Parts = []message.ContentPart{
			message.CommandContent{Command: command, Output: output, ExitCode: &exitCode, Pending: false},
		}
		if err := state.messages.Update(state.ctx, *state.pendingResult); err != nil {
			slog.Warn("handleCommandResult: failed to update pending result", "error", err)
		}
		state.pendingResult = nil
	} else {
		// Defensive fallback: create a new result row.
		_, err := state.messages.Create(state.ctx, state.sessionID, message.CreateMessageParams{
			Role:  message.Result,
			Parts: []message.ContentPart{message.CommandContent{Command: command, Output: output, ExitCode: &exitCode, Pending: false}},
		})
		if err != nil {
			slog.Warn("handleCommandResult: failed to create fallback result", "error", err)
		}
	}
}

func (state *runState) handleTurnEnd(reason logos.StopReason) {
	var finish message.FinishReason
	switch reason {
	case logos.StopReasonFinal:
		finish = message.FinishReasonEndTurn
	case logos.StopReasonCanceled:
		finish = message.FinishReasonCanceled
	case logos.StopReasonError, logos.StopReasonHallucinationLimit, logos.StopReasonMaxSteps:
		finish = message.FinishReasonError
	}

	if state.currentAssistant != nil {
		state.currentAssistant.AddFinish(finish, "", "")
		if err := state.messages.Update(state.ctx, *state.currentAssistant); err != nil {
			slog.Warn("handleTurnEnd: failed to persist finish", "error", err)
		}
	}

	// Abandon any pending result (cancel/error mid-cmd).
	if state.pendingResult != nil {
		exitCode := -1
		state.pendingResult.Parts = []message.ContentPart{
			message.CommandContent{Command: "", Output: "canceled before result", ExitCode: &exitCode, Pending: false},
		}
		if err := state.messages.Update(state.ctx, *state.pendingResult); err != nil {
			slog.Warn("handleTurnEnd: failed to abandon pending result", "error", err)
		}
		state.pendingResult = nil
	}
}

func (a *sessionAgent) Run(ctx context.Context, call SessionAgentCall) (*logos.RunResult, error) {
	var state *runState

runLoop:
	// Reset state to prevent stale-message misalignment on queued turns.
	state = &runState{
		sessionID:  call.SessionID,
		providerID: call.ProviderID,
		logosCfg:   call.LogosCfg,
		messages:   a.messages,
		ctx:        ctx,
	}

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

	history := buildHistory(msgs)

	startTime := time.Now()
	a.eventPromptSent(call.SessionID)

	callbacks := logos.Callbacks{
		OnStepStart:          state.handleStepStart,
		OnStepEnd:            state.handleStepEnd,
		OnDelta:              state.handleDelta,
		OnReasoningDelta:     state.handleReasoningDelta,
		OnReasoningSignature: state.handleReasoningSignature,
		OnCommandResult:      state.handleCommandResult,
		OnTurnEnd:            state.handleTurnEnd,
	}

	result, runErr := logos.Run(streamCtx, call.LogosCfg, history, call.Prompt, callbacks)

	a.eventPromptResponded(call.SessionID, time.Since(startTime).Truncate(time.Second))

	if runErr != nil {
		// Add user-facing error feedback to the assistant message.
		// handleTurnEnd already set FinishReasonError with empty strings;
		// AddFinish replaces it with the detailed message.
		if state.currentAssistant != nil {
			_, title, detail := errorFinishFor(runErr, call.LogosCfg.Model)
			state.currentAssistant.AddFinish(message.FinishReasonError, title, detail)
			if updateErr := a.messages.Update(ctx, *state.currentAssistant); updateErr != nil {
				slog.Warn("Failed to update assistant message on error", "error", updateErr)
			}
		}

		// Still save usage on cancellation (result is non-nil for cancel).
		if result != nil {
			if s, ok := a.saveSessionUsage(ctx, call.SessionID, result, "Failed to save session usage on cancellation"); ok {
				currentSession = s
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

	// Update session usage from logos result (context %, cost).
	if result != nil {
		if updatedSession, ok := a.saveSessionUsage(ctx, call.SessionID, result, "Failed to save session usage"); ok {
			currentSession = updatedSession
		}
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
