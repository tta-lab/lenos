package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"charm.land/fantasy"

	"github.com/tta-lab/lenos/internal/agent/notify"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/pubsub"
)

// resolveRunner picks LocalRunner or SandboxRunner from the call context.
// On fallback to LocalRunner it logs a clear warning so the operator sees the
// security implication (subprocess inherits parent env including secrets).
func resolveRunner(call SessionAgentCall, bg *BackgroundRunner) Runner {
	if call.Sandbox {
		return &SandboxedRunner{bg: bg}
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
	bgRunner := a.getOrCreateBackgroundRunner(call)
	defer a.cleanupBackgroundRunner(call.SessionID, bgRunner)

	runner := resolveRunner(call, bgRunner)

	primaryModel := a.primaryModel.Get()

	msgs, err := a.getSessionMessages(ctx, currentSession)
	if err != nil {
		return fmt.Errorf("failed to get session messages: %w", err)
	}
	isNewSession := len(msgs) == 0
	if isNewSession && currentSession.SummaryMessageID != "" && len(call.ContextCommands) > 0 {
		call.ContextCommands = appendCompactRuntimeContextCommand(call.ContextCommands)
	}
	if isNewSession && len(call.ContextCommands) > 0 {
		if err := a.persistRuntimeContextCommands(ctx, call, runner); err != nil {
			return err
		}
		msgs, err = a.getSessionMessages(ctx, currentSession)
		if err != nil {
			return fmt.Errorf("failed to get session messages after context injection: %w", err)
		}
	}

	turnPrompts := turnPromptsForCall(call)

	// Inject journal guidance on the first session turn. The prompt, not Go
	// code, decides whether the current user request is task-like.
	if isNewSession && currentSession.SummaryMessageID == "" && call.JournalPath != "" {
		turnPrompts = append(turnPrompts, turnPrompt{
			Text:    taskDetectionHint(),
			Persist: true,
			Role:    message.Runtime,
		})
	}
	if err := a.persistVisibleTurnPrompts(ctx, call.SessionID, turnPrompts); err != nil {
		return err
	}

	var wg sync.WaitGroup
	titleCtx := ctx
	wg.Go(func() {
		a.generateTitle(titleCtx, call.SessionID, call.TaskID)
	})
	defer wg.Wait()

	streamCtx, cancel := context.WithCancel(ctx)
	a.activeRequests.Set(call.SessionID, cancel)
	defer cancel()
	defer a.activeRequests.Del(call.SessionID)

	history := buildHistory(msgs)
	startTime := time.Now()
	a.eventPromptSent(call.SessionID)

	deps := loopDeps{
		model:        primaryModel,
		provOpts:     call.ProviderOptions,
		pairWith:     call.PairWith,
		messages:     a.messages,
		runner:       runner,
		sessionID:    call.SessionID,
		sysPrompt:    a.systemPrompt.Get(),
		env:          call.Env,
		paths:        call.AllowedPaths,
		bgRunner:     bgRunner,
		postStepHook: a.buildPostStepHook(call, primaryModel),
		onUsage: func() func(int, fantasy.Usage, fantasy.ProviderMetadata) {
			var cumulative int64
			var autoCompactDone bool
			interval := call.JournalCheckIntervalTokens
			hasJournal := call.JournalPath != ""
			return func(_ int, u fantasy.Usage, m fantasy.ProviderMetadata) {
				overrideCost := a.openrouterCost(m)
				s, ok := a.saveSessionUsage(streamCtx, call.SessionID, u, m, "Failed to save session usage at step")
				if !ok {
					return
				}
				currentSession = s
				if call.usageSummary != nil {
					call.usageSummary.AddUsage(primaryModel, u, usageCost(primaryModel, u, overrideCost))
				}
				if hasJournal {
					if interval > 0 {
						prev := cumulative / int64(interval)
						cumulative += u.InputTokens
						cur := cumulative / int64(interval)
						if cur > prev {
							a.injectRuntimePrompt(call, periodicCheckHint())
						}
					}
					if !autoCompactDone {
						totalUsed := currentSession.PromptTokens + currentSession.CompletionTokens
						if ctxWin := primaryModel.CatwalkCfg.ContextWindow; ctxWin > 0 && totalUsed >= int64(ctxWin)*80/100 {
							autoCompactDone = true
							call.MarkCompactBoundary = true
							a.injectRuntimePrompt(call, autoCompactHint())
						}
					}
				}
			}
		}(),
		drainQueue: func() []turnPrompt {
			queued, ok := a.messageQueue.Take(call.SessionID)
			if !ok || len(queued) == 0 {
				return nil
			}
			return turnPromptsForCall(combineQueuedCalls(queued))
		},
	}

	stop, runErr := runLoopWithPrompts(streamCtx, deps, history, turnPrompts)

	a.eventPromptResponded(call.SessionID, time.Since(startTime).Truncate(time.Second))

	_ = stop

	if runErr != nil {
		// Release activeRequests before surfacing the error so
		// IsSessionBusy returns false. Ported from upstream 9d346688.
		cancel()
		a.activeRequests.Del(call.SessionID)

		a.attachErrorFinish(ctx, call.SessionID, runErr, primaryModel.Model.Model())

		if newCall, ok := a.tryReenter(call, cancel); ok {
			call = newCall
			goto runLoopReentry
		}
		return runErr
	}

	// Release activeRequests before publishing TypeAgentFinished so
	// IsSessionBusy returns false when the subscriber processes the event.
	// Ported from upstream 9d346688.
	cancel()
	a.activeRequests.Del(call.SessionID)

	if a.notify != nil {
		a.notify.Publish(pubsub.CreatedEvent, notify.Notification{
			SessionID:    call.SessionID,
			SessionTitle: currentSession.Title,
			Type:         notify.TypeAgentFinished,
		})
	}

	// Mark compaction boundary if requested. This sets SummaryMessageID so
	// future turns load only post-boundary history (fresh context window).
	if call.MarkCompactBoundary {
		currentSession.SummaryMessageID = getLastAssistantID(ctx, a.messages, call.SessionID)
		if currentSession.SummaryMessageID != "" {
			currentSession.PromptTokens = 0
			currentSession.CompletionTokens = 0
			currentSession.CacheCreationTokens = 0
			currentSession.CacheReadTokens = 0
			currentSession.CacheMissTokens = 0
			if _, err := a.sessions.Save(ctx, currentSession); err != nil {
				slog.Warn("compact boundary: failed to save session", "session", call.SessionID, "error", err)
			}
		}
	}

	if newCall, ok := a.tryReenter(call, cancel); ok {
		call = newCall
		goto runLoopReentry
	}
	return nil
}
