package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"charm.land/fantasy"
	"charm.land/lipgloss/v2"

	"github.com/tta-lab/lenos/internal/agent/hyper"
	"github.com/tta-lab/lenos/internal/agent/notify"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/pubsub"
	"github.com/tta-lab/lenos/internal/stringext"
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

	var (
		lastUsage fantasy.Usage
		lastMeta  fantasy.ProviderMetadata
		usageSeen bool
	)

	largeModel := a.largeModel.Get()
	deps := loopDeps{
		model:      largeModel.Model,
		provOpts:   call.ProviderOptions,
		messages:   a.messages,
		runner:     resolveRunner(call),
		recorder:   a.recorder,
		sessionID:  call.SessionID,
		sysPrompt:  a.systemPrompt.Get(),
		providerID: call.ProviderID,
		env:        call.Env,
		paths:      call.AllowedPaths,
		onUsage: func(_ int, u fantasy.Usage, m fantasy.ProviderMetadata) {
			lastUsage = u
			lastMeta = m
			usageSeen = true
		},
	}

	_, runErr := runLoop(streamCtx, deps, history, call.Prompt)

	a.eventPromptResponded(call.SessionID, time.Since(startTime).Truncate(time.Second))

	if runErr == nil && !usageSeen {
		slog.Warn("agent loop completed without usage callback",
			"session_id", call.SessionID, "model", largeModel.Model.Model())
	}

	if runErr != nil {
		// Loop already persisted partial work; surface a user-facing finish
		// on the most-recent assistant message so the UI shows actionable
		// guidance (e.g. "Copilot model not enabled").
		a.attachErrorFinish(ctx, call.SessionID, runErr, largeModel.Model.Model())

		if usageSeen {
			if s, ok := a.saveSessionUsage(ctx, call.SessionID, lastUsage, lastMeta, "Failed to save session usage on cancellation"); ok {
				currentSession = s
			}
		}

		a.activeRequests.Del(call.SessionID)
		cancel()

		queuedMessages, ok := a.messageQueue.Get(call.SessionID)
		if ok && len(queuedMessages) > 0 {
			nextCall := queuedMessages[0]
			a.messageQueue.Set(call.SessionID, queuedMessages[1:])
			call = nextCall
			goto runLoopReentry
		}
		return runErr
	}

	if usageSeen {
		if updatedSession, ok := a.saveSessionUsage(ctx, call.SessionID, lastUsage, lastMeta, "Failed to save session usage"); ok {
			currentSession = updatedSession
		}
	}

	if a.notify != nil {
		a.notify.Publish(pubsub.CreatedEvent, notify.Notification{
			SessionID:    call.SessionID,
			SessionTitle: currentSession.Title,
			Type:         notify.TypeAgentFinished,
		})
	}

	a.activeRequests.Del(call.SessionID)
	cancel()

	queuedMessages, ok := a.messageQueue.Get(call.SessionID)
	if !ok || len(queuedMessages) == 0 {
		return nil
	}
	nextCall := queuedMessages[0]
	a.messageQueue.Set(call.SessionID, queuedMessages[1:])
	call = nextCall
	goto runLoopReentry
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
