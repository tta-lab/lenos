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
	"github.com/tta-lab/lenos/internal/transcript"
)

// buildHistory converts session messages to fantasy messages for the bash-first
// loop. The current-turn prompt is NOT included — runLoop appends it before
// calling the model.
func buildHistory(msgs []message.Message) []fantasy.Message {
	history := make([]fantasy.Message, 0, len(msgs))
	for _, m := range msgs {
		history = append(history, m.ToAIMessage()...)
	}
	return history
}

const queuedPromptSep = "\n\n"

// errorFinishFor returns an appropriate FinishReason and user-facing message
// for a run error. This provides actionable feedback (e.g. "enable Copilot
// model", "add credits") rather than opaque error strings.
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

// resolveRunner picks LocalRunner or SandboxRunner from the call context.
// On fallback to LocalRunner it logs a clear warning so the operator sees the
// security implication (subprocess inherits parent env including secrets).
func resolveRunner(call SessionAgentCall) Runner {
	if call.Sandbox && call.SandboxClient != nil {
		return SandboxRunner{Client: call.SandboxClient}
	}
	if call.Sandbox && call.SandboxClient == nil {
		slog.Warn("sandbox requested but client is nil; falling back to LocalRunner — bash subprocess inherits parent env including secrets",
			"session_id", call.SessionID)
	}
	return LocalRunner{}
}

func (a *sessionAgent) Run(ctx context.Context, call SessionAgentCall) error {
runLoopReentry:
	if call.Prompt == "" {
		return ErrEmptyPrompt
	}
	if call.SessionID == "" {
		return ErrSessionMissing
	}

	if a.IsSessionBusy(call.SessionID) {
		existing, ok := a.messageQueue.Get(call.SessionID)
		if !ok {
			existing = []SessionAgentCall{}
		}
		existing = append(existing, call)
		a.messageQueue.Set(call.SessionID, existing)
		return nil
	}

	currentSession, err := a.sessions.Get(ctx, call.SessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	msgs, err := a.getSessionMessages(ctx, currentSession)
	if err != nil {
		return fmt.Errorf("failed to get session messages: %w", err)
	}

	if _, err := a.messages.Create(ctx, call.SessionID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: call.Prompt}},
	}); err != nil {
		return fmt.Errorf("failed to create user message: %w", err)
	}

	var wg sync.WaitGroup
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

	largeModel := a.largeModel.Get()
	rec := call.Recorder
	if rec == nil {
		rec = a.recorder
	}
	slog.Info("agent.Run env diagnostic",
		"session_id", call.SessionID,
		"env_LENOS_SESSION_ID", call.Env["LENOS_SESSION_ID"],
		"env_count", len(call.Env),
		"runner", fmt.Sprintf("%T", resolveRunner(call)),
		"sysprompt_len", len(a.systemPrompt.Get()),
	)
	deps := loopDeps{
		model:      largeModel.Model,
		provOpts:   call.ProviderOptions,
		messages:   a.messages,
		runner:     resolveRunner(call),
		recorder:   transcript.NewLoggingRecorder(rec),
		sessionID:  call.SessionID,
		sysPrompt:  a.systemPrompt.Get(),
		providerID: call.ProviderID,
		env:        call.Env,
		paths:      call.AllowedPaths,
		onUsage: func(_ int, u fantasy.Usage, m fantasy.ProviderMetadata) bool {
			s, ok := a.saveSessionUsage(streamCtx, call.SessionID, u, m, "Failed to save session usage at step")
			if !ok {
				return false
			}
			currentSession = s
			if a.disableAutoSummarize {
				return false
			}
			used := s.PromptTokens + s.CompletionTokens
			return shouldAutoCompact(int64(largeModel.CatwalkCfg.ContextWindow), used)
		},
		drainQueue: func() []string {
			queued, ok := a.messageQueue.Take(call.SessionID)
			if !ok || len(queued) == 0 {
				return nil
			}
			prompts := make([]string, len(queued))
			for i, q := range queued {
				prompts[i] = q.Prompt
			}
			return prompts
		},
	}

	stop, runErr := runLoop(streamCtx, deps, history, call.Prompt)

	a.eventPromptResponded(call.SessionID, time.Since(startTime).Truncate(time.Second))

	if runErr == nil && stop == stopShouldSummarize {
		// Release the runLoop context and remove from activeRequests so Summarize
		// can use IsSessionBusy guard (Summarize sets its own entry).
		cancel()
		a.activeRequests.Del(call.SessionID)
		if summarizeErr := a.Summarize(ctx, call.SessionID, call.ProviderOptions); summarizeErr != nil {
			return summarizeErr
		}
		call.Prompt = fmt.Sprintf(
			"The previous session was interrupted because it got too long, the initial user request was: `%s`",
			call.Prompt,
		)
		goto runLoopReentry
	}

	if runErr != nil {
		// Loop already persisted partial work; surface a user-facing finish
		// on the most-recent assistant message so the UI shows actionable
		// guidance (e.g. "Copilot model not enabled").
		a.attachErrorFinish(ctx, call.SessionID, runErr, largeModel.Model.Model())

		if newCall, ok := a.tryReenter(call, cancel); ok {
			call = newCall
			goto runLoopReentry
		}
		return runErr
	}

	if a.notify != nil {
		a.notify.Publish(pubsub.CreatedEvent, notify.Notification{
			SessionID:    call.SessionID,
			SessionTitle: currentSession.Title,
			Type:         notify.TypeAgentFinished,
		})
	}

	if newCall, ok := a.tryReenter(call, cancel); ok {
		call = newCall
		goto runLoopReentry
	}
	return nil
}

// attachErrorFinish updates the most-recent assistant message in the session
// with a user-facing FinishReasonError + title + detail derived from the
// loop's run error. The loop creates assistant rows as it streams; this
// follow-up replaces any tool-use/end-turn finish on the LAST one with an
// error-flavored finish so the UI banner makes sense.
func (a *sessionAgent) attachErrorFinish(ctx context.Context, sessionID string, runErr error, model string) {
	all, listErr := a.messages.List(ctx, sessionID)
	if listErr != nil {
		slog.Warn("attachErrorFinish: list messages", "error", listErr)
		return
	}
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Role != message.Assistant {
			continue
		}
		latest := all[i]
		_, title, detail := errorFinishFor(runErr, model)
		latest.AddFinish(message.FinishReasonError, title, detail)
		if updateErr := a.messages.Update(ctx, latest); updateErr != nil {
			slog.Warn("attachErrorFinish: update", "error", updateErr)
		}
		return
	}
}

// combineQueuedCalls collapses N queued calls into one re-entry call.
// Prompts join with "\n\n"; runtime fields take from the FIRST queued call.
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
	sb.WriteString(first.Prompt)
	for _, c := range calls[1:] {
		sb.WriteString(queuedPromptSep)
		sb.WriteString(c.Prompt)
	}
	first.Prompt = sb.String()
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
