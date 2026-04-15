package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"charm.land/fantasy"

	"github.com/tta-lab/lenos/internal/agent/notify"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/pubsub"
	"github.com/tta-lab/logos/v2"
)

// runState holds mutable state for the duration of a single Run() call.
// Each queued turn reinitializes these fields via the runLoop reset block.
type runState struct {
	sessionID string
	logosCfg  logos.Config
	messages  message.Service
	ctx       context.Context

	currentAssistant *message.Message
	pendingResult    *message.Message
}

func (state *runState) handleStepStart(stepIdx int) {
	state.currentAssistant = nil
	providerName := ""
	if state.logosCfg.Provider != nil {
		providerName = state.logosCfg.Provider.Name()
	}
	msg, err := state.messages.Create(state.ctx, state.sessionID, message.CreateMessageParams{
		Role:     message.Assistant,
		Parts:    []message.ContentPart{message.TextContent{Text: ""}},
		Model:    state.logosCfg.Model,
		Provider: providerName,
	})
	if err != nil {
		slog.Warn("handleStepStart: failed to create assistant message", "error", err)
		return
	}
	state.currentAssistant = &msg
}

func (state *runState) handleStepEnd(stepIdx int) {
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
		slog.Warn("handleDelta: no currentAssistant, creating placeholder", "text_prefix", text[:30])
		state.handleStepStart(0)
		if state.currentAssistant == nil {
			return
		}
	}

	if strings.HasPrefix(text, "<cmd>") {
		cmdText, _, _ := strings.Cut(text, "</cmd>")
		cmdText = strings.TrimPrefix(cmdText, "<cmd>")
		cmdText = strings.TrimSpace(cmdText)
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
		state.currentAssistant = nil
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
		sessionID: call.SessionID,
		logosCfg:  call.LogosCfg,
		messages:  a.messages,
		ctx:       ctx,
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

	history := make([]fantasy.Message, 0, len(msgs))
	for _, m := range msgs {
		history = append(history, m.ToAIMessage()...)
	}
	// Add the user message to history so the LLM sees the full context.
	history = append(history, fantasy.NewUserMessage(call.Prompt))

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
