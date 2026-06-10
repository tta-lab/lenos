package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/tta-lab/lenos/internal/message"
)

const queuedPromptSep = "\n\n"

func turnPromptsForCall(call SessionAgentCall) []turnPrompt {
	if len(call.turnPrompts) > 0 {
		return call.turnPrompts
	}
	role := message.User
	if call.runtimePrompt {
		role = message.Runtime
	}
	return []turnPrompt{{
		Text:    call.Prompt,
		Persist: true,
		Role:    role,
	}}
}

func (a *sessionAgent) persistVisibleTurnPrompts(ctx context.Context, sessionID string, prompts []turnPrompt) error {
	for _, prompt := range prompts {
		if !prompt.Persist {
			continue
		}
		role := prompt.Role
		if role == "" {
			role = message.User
		}
		if _, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
			Role:  role,
			Parts: []message.ContentPart{message.TextContent{Text: prompt.Text}},
		}); err != nil {
			return fmt.Errorf("failed to create user message: %w", err)
		}
	}
	return nil
}

// combineQueuedCalls collapses N queued calls into one re-entry call.
// Prompts join with "\n\n"; per-prompt roles are preserved in turnPrompts.
// runtimePrompt is cleared because turnPrompts becomes the role SSOT for the
// combined call.
// MarkCompactBoundary is ORed across all calls so compaction is never lost.
// Caller must check len(calls) > 0 before invoking.
func combineQueuedCalls(calls []SessionAgentCall) SessionAgentCall {
	if len(calls) == 0 {
		panic("combineQueuedCalls: calls must be non-empty")
	}
	first := calls[0]
	if len(calls) == 1 {
		return first
	}
	var sb strings.Builder
	prompts := make([]turnPrompt, 0, len(calls))
	compact := first.MarkCompactBoundary
	for i, c := range calls {
		if i > 0 {
			sb.WriteString(queuedPromptSep)
		}
		sb.WriteString(c.Prompt)
		prompts = append(prompts, turnPromptsForCall(c)...)
		compact = compact || c.MarkCompactBoundary
	}
	first.Prompt = sb.String()
	first.turnPrompts = prompts
	first.runtimePrompt = false
	first.MarkCompactBoundary = compact
	return first
}

// tryReenter clears the session from activeRequests, cancels the streaming
// context, and attempts to drain the message queue. Returns the re-entry call
// and true if a re-entry should happen; returns (call, false) if the queue is
// empty or absent so the caller can return/continue as appropriate.
func (a *sessionAgent) tryReenter(call SessionAgentCall, cancel context.CancelFunc) (SessionAgentCall, bool) {
	a.activeRequests.Del(call.SessionID)
	cancel()
	queued, ok := a.messageQueue.Take(call.SessionID)
	if !ok || len(queued) == 0 {
		return call, false
	}
	return combineQueuedCalls(queued), true
}

func (a *sessionAgent) injectRuntimePrompt(call SessionAgentCall, msg string) {
	runtimeCall := SessionAgentCall{
		SessionID:           call.SessionID,
		Prompt:              msg,
		runtimePrompt:       true,
		MarkCompactBoundary: call.MarkCompactBoundary,
		ProviderOptions:     call.ProviderOptions,
		Sandbox:             call.Sandbox,
		Env:                 call.Env,
		AllowedPaths:        call.AllowedPaths,
	}
	if a.IsSessionBusy(call.SessionID) {
		existing, _ := a.messageQueue.Get(call.SessionID)
		existing = append(existing, runtimeCall)
		a.messageQueue.Set(call.SessionID, existing)
		return
	}
	go func() {
		if err := a.Run(context.Background(), runtimeCall); err != nil {
			slog.Warn("runtime prompt injection: run failed", "session_id", call.SessionID, "error", err)
		}
	}()
}

func (a *sessionAgent) enqueueBackgroundJobResult(call SessionAgentCall) func(msg string) {
	return func(msg string) {
		runtimeCall := SessionAgentCall{
			SessionID:              call.SessionID,
			Prompt:                 msg,
			runtimePrompt:          true,
			ProviderOptions:        call.ProviderOptions,
			Sandbox:                call.Sandbox,
			Env:                    call.Env,
			AllowedPaths:           call.AllowedPaths,
			trajectoryMaterializer: call.trajectoryMaterializer,
		}
		if a.IsSessionBusy(call.SessionID) {
			existing, _ := a.messageQueue.Get(call.SessionID)
			existing = append(existing, runtimeCall)
			a.messageQueue.Set(call.SessionID, existing)
			return
		}
		go func() {
			if err := a.Run(context.Background(), runtimeCall); err != nil {
				slog.Warn("background job result: run failed", "session_id", call.SessionID, "error", err)
			}
		}()
	}
}
