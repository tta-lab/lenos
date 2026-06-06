package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"charm.land/fantasy"
	"charm.land/lipgloss/v2"

	"github.com/tta-lab/lenos/internal/agent/hyper"
	"github.com/tta-lab/lenos/internal/agent/lenosbash"
	"github.com/tta-lab/lenos/internal/agent/notify"
	"github.com/tta-lab/lenos/internal/hooks"
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

const queuedPromptSep = "\n\n"

func (a *sessionAgent) persistRuntimeContextCommands(ctx context.Context, call SessionAgentCall, runner Runner) error {
	for _, cmd := range call.ContextCommands {
		if strings.TrimSpace(cmd.Command) == "" {
			continue
		}
		if err := a.persistSyntheticCommandResult(ctx, call, runner, cmd); err != nil {
			return err
		}
	}
	return nil
}

func (a *sessionAgent) persistSyntheticCommandResult(ctx context.Context, call SessionAgentCall, runner Runner, cmd RuntimeContextCommand) error {
	if handled, err := a.persistSyntheticProse(ctx, call, cmd.Command); handled || err != nil {
		return err
	}

	commandForBash := cmd.Command
	if parsed, diag := lenosbash.Parse(cmd.Command); diag == nil && len(parsed.Bash) > 0 {
		commandForBash = parsed.Bash[0]
	}

	res := runner.Run(ctx, commandForBash, call.Env, call.AllowedPaths)
	if cmd.Optional && (res.Err != nil || res.ExitCode != 0 || strings.TrimSpace(string(res.Stdout)) == "") {
		return nil
	}

	assistantMsg, err := a.messages.Create(ctx, call.SessionID, message.CreateMessageParams{
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: cmd.Command},
		},
	})
	if err != nil {
		return fmt.Errorf("create synthetic context command: %w", err)
	}

	resultMsg, err := a.messages.Create(ctx, call.SessionID, message.CreateMessageParams{
		Role:  message.Result,
		Parts: []message.ContentPart{message.CommandContent{Command: commandForBash, Pending: true}},
	})
	if err != nil {
		return fmt.Errorf("create synthetic context pending result: %w", err)
	}

	exitCode := res.ExitCode
	stderr := res.Stderr
	if res.Err != nil && len(res.Stdout) == 0 && len(stderr) == 0 {
		stderr = []byte(res.Err.Error())
	}
	envelope := formatResultForModel(commandForBash, string(res.Stdout), string(stderr), res.ExitCode)
	body := lenosbash.ResultBody(envelope)
	resultMsg.Parts = []message.ContentPart{message.CommandContent{
		Command:     commandForBash,
		Output:      string(combine(res.Stdout, stderr)),
		ExitCode:    &exitCode,
		Pending:     false,
		Observation: body,
	}}
	if err := a.messages.Update(ctx, resultMsg); err != nil {
		return fmt.Errorf("update synthetic context result: %w", err)
	}
	markStepFinished(ctx, a.loopDepsForSynthetic(call.SessionID), &assistantMsg, message.FinishReasonToolUse)
	return nil
}

func (a *sessionAgent) persistSyntheticProse(ctx context.Context, call SessionAgentCall, command string) (bool, error) {
	parsed, diag := lenosbash.Parse(command)
	if diag != nil {
		return false, nil
	}
	if strings.TrimSpace(parsed.Prose) == "" || len(parsed.Bash) > 0 {
		return false, nil
	}
	model := a.primaryModel.Get()
	if _, err := a.messages.Create(ctx, call.SessionID, message.CreateMessageParams{
		Role:     message.Assistant,
		Parts:    []message.ContentPart{message.TextContent{Text: command}},
		Model:    model.messageModelID(),
		Provider: model.messageProviderID(),
	}); err != nil {
		return true, fmt.Errorf("create synthetic prose: %w", err)
	}
	return true, nil
}

func (a *sessionAgent) loopDepsForSynthetic(sessionID string) loopDeps {
	return loopDeps{
		messages:  a.messages,
		sessionID: sessionID,
	}
}

// hookTimeout is the per-invocation deadline for post_step hooks. Var (not
// const) so tests can shrink it via export_test.go.
var hookTimeout = 5 * time.Second

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

	// Inject journal system hint on first turn for task sessions.
	if isNewSession && call.JournalPath != "" {
		hint := journalSystemHint(call.JournalPath)
		turnPrompts = append([]turnPrompt{{Text: hint, Persist: true}}, turnPrompts...)

		// Inject task detection hint when the prompt looks like a task.
		if isTaskLike(call.Prompt) {
			taskHint := taskDetectionHint()
			turnPrompts = append(turnPrompts, turnPrompt{Text: taskHint, Persist: false})
		}
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

	// If a journal is active, print path at exit so the user knows where it is.
	if call.JournalPath != "" {
		defer func() {
			fmt.Fprintf(os.Stderr, "%s\n", journalExitSummary(call.JournalPath))
		}()
	}

	history := buildHistory(msgs)
	startTime := time.Now()
	a.eventPromptSent(call.SessionID)

	// postStepHook builds and fires the configured post_step hook, if any.
	// Runs in a goroutine with hookTimeout deadline; errors are logged at
	// WARN but never abort the loop.
	var postStepHook func(stepIdx int, u fantasy.Usage, m fantasy.ProviderMetadata)
	if a.hookRunner != nil {
		runner := a.hookRunner
		sessionID := call.SessionID
		modelID := primaryModel.Model.Model()
		contextWindow := int(primaryModel.CatwalkCfg.ContextWindow)
		postStepHook = func(stepIdx int, u fantasy.Usage, m fantasy.ProviderMetadata) {
			payload, err := hooks.MarshalPostStep(stepIdx, sessionID, modelID, contextWindow, u, time.Now(), usageCost(primaryModel, u, a.openrouterCost(m)))
			if err != nil {
				slog.Warn("post_step: marshal envelope", "session", sessionID, "step", stepIdx, "error", err)
				return
			}
			timeout := hookTimeout // capture at closure-execution time, before spawning goroutine
			go func() {
				hookCtx, cancel := context.WithTimeout(context.Background(), timeout)
				defer cancel()
				if err := runner.Run(hookCtx, payload); err != nil {
					slog.Warn("post_step: runner failed", "session", sessionID, "step", stepIdx, "error", err)
				}
			}()
		}
	}
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
		postStepHook: postStepHook,
		onUsage: func(_ int, u fantasy.Usage, m fantasy.ProviderMetadata) {
			overrideCost := a.openrouterCost(m)
			s, ok := a.saveSessionUsage(streamCtx, call.SessionID, u, m, "Failed to save session usage at step")
			if !ok {
				return
			}
			currentSession = s
			if call.usageSummary != nil {
				call.usageSummary.AddUsage(primaryModel, u, usageCost(primaryModel, u, overrideCost))
			}
		},
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
//
// Boundary note: attach the error banner to the newest durable assistant row
// rather than assuming every streamed emit still exists.
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

func turnPromptsForCall(call SessionAgentCall) []turnPrompt {
	if len(call.turnPrompts) > 0 {
		return call.turnPrompts
	}
	return []turnPrompt{{
		Text:    call.Prompt,
		Persist: !call.runtimePrompt,
	}}
}

func (a *sessionAgent) persistVisibleTurnPrompts(ctx context.Context, sessionID string, prompts []turnPrompt) error {
	for _, prompt := range prompts {
		if !prompt.Persist {
			continue
		}
		if _, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: prompt.Text}},
		}); err != nil {
			return fmt.Errorf("failed to create user message: %w", err)
		}
	}
	return nil
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
	prompts := make([]turnPrompt, 0, len(calls))
	for i, c := range calls {
		if i > 0 {
			sb.WriteString(queuedPromptSep)
		}
		sb.WriteString(c.Prompt)
		prompts = append(prompts, turnPromptsForCall(c)...)
	}
	first.Prompt = sb.String()
	first.turnPrompts = prompts
	first.runtimePrompt = false
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

func (a *sessionAgent) getOrCreateBackgroundRunner(call SessionAgentCall) *BackgroundRunner {
	a.bgRunnersMu.Lock()
	defer a.bgRunnersMu.Unlock()
	if br, ok := a.bgRunners.Get(call.SessionID); ok && br != nil {
		return br
	}
	br := NewBackgroundRunner(a.enqueueBackgroundJobResult(call))
	setOnIdle(br, func() {
		a.bgRunnersMu.Lock()
		a.bgRunners.Del(call.SessionID)
		a.bgRunnersMu.Unlock()
	})
	a.bgRunners.Set(call.SessionID, br)
	return br
}

// setOnIdle writes the onIdle callback under onIdleMu to prevent races
// with the background goroutine in Track that reads it.
func setOnIdle(br *BackgroundRunner, f func()) {
	br.onIdleMu.Lock()
	br.onIdle = f
	br.onIdleMu.Unlock()
}

func (a *sessionAgent) cleanupBackgroundRunner(sessionID string, br *BackgroundRunner) {
	if br.ActiveCount() > 0 {
		a.bgRunners.Set(sessionID, br)
		return
	}
	a.bgRunnersMu.Lock()
	if current, ok := a.bgRunners.Get(sessionID); ok && current == br {
		a.bgRunners.Del(sessionID)
	}
	a.bgRunnersMu.Unlock()
}

func (a *sessionAgent) enqueueBackgroundJobResult(call SessionAgentCall) func(msg string) {
	return func(msg string) {
		runtimeCall := SessionAgentCall{
			SessionID:       call.SessionID,
			Prompt:          msg,
			runtimePrompt:   true,
			ProviderOptions: call.ProviderOptions,
			Sandbox:         call.Sandbox,
			Env:             call.Env,
			AllowedPaths:    call.AllowedPaths,
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
