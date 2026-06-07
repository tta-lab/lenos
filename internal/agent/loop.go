package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/tta-lab/lenos/internal/agent/lenosbash"
	"github.com/tta-lab/lenos/internal/message"
)

const (
	// StepCap bounds how many model emissions one Run() call can issue.
	StepCap = 500
)

// ErrStepCap signals that the loop halted because the model issued StepCap
// emissions without hitting an 'exit'.
var ErrStepCap = errors.New("agent: step cap reached")

// loopDeps wires the bash-first loop to its environment.
type loopDeps struct {
	model        Model
	drainQueue   func() []turnPrompt
	provOpts     fantasy.ProviderOptions
	pairWith     string
	messages     message.Service
	runner       Runner
	sessionID    string
	sysPrompt    string
	env          map[string]string
	paths        []AllowedPath
	onUsage      func(stepIdx int, u fantasy.Usage, m fantasy.ProviderMetadata)
	postStepHook func(stepIdx int, u fantasy.Usage, m fantasy.ProviderMetadata)
	bgRunner     *BackgroundRunner
}

// stopReason explains why runLoop returned.
type stopReason int

const (
	stopEndTurn stopReason = iota
	stopStepCap
	stopError
	stopCanceled
)

// runLoop drives one turn: stream → parse → execute run blocks → repeat.
func runLoop(ctx context.Context, deps loopDeps, history []fantasy.Message, prompt string) (stopReason, error) {
	return runLoopWithPrompts(ctx, deps, history, []turnPrompt{{Text: prompt}})
}

func runLoopWithPrompts(ctx context.Context, deps loopDeps, history []fantasy.Message, prompts []turnPrompt) (stopReason, error) {
	msgs := make([]fantasy.Message, 0, len(history)+1+len(prompts))
	msgs = append(msgs, fantasy.NewSystemMessage(deps.sysPrompt))
	msgs = append(msgs, history...)
	for _, prompt := range prompts {
		msgs = append(msgs, turnPromptMessage(prompt))
	}
	for step := 0; step < StepCap; step++ {
		assistantMsg, err := deps.messages.Create(ctx, deps.sessionID, message.CreateMessageParams{
			Role:     message.Assistant,
			Parts:    []message.ContentPart{message.TextContent{Text: ""}},
			Model:    deps.model.messageModelID(),
			Provider: deps.model.messageProviderID(),
		})
		if err != nil {
			return stopError, fmt.Errorf("create assistant message: %w", err)
		}
		emit, usage, meta, streamErr := streamOne(ctx, deps, msgs, &assistantMsg)
		if streamErr != nil {
			if isCanceled(streamErr) {
				return stopCanceled, streamErr
			}
			return stopError, streamErr
		}
		if deps.postStepHook != nil {
			deps.postStepHook(step, usage, meta)
		}
		if deps.onUsage != nil {
			deps.onUsage(step, usage, meta)
		}
		parsed, diag := lenosbash.Parse(emit)
		// Handle parse errors.
		if diag != nil {
			obs := lenosbash.RenderDiagnostic(emit, *diag)
			msgs = append(msgs,
				assistantTextMessage(emit, assistantMsg.ReasoningContent()),
				fantasy.NewUserMessage(obs),
			)
			if obsErr := persistObservation(ctx, deps, obs); obsErr != nil {
				slog.Warn("loop: persist tag-diag re-prompt", "error", obsErr)
			}
			markStepFinished(ctx, deps, &assistantMsg, message.FinishReasonToolUse)
			msgs, _ = drainAndAppend(ctx, deps, msgs)
			continue
		}
		if len(parsed.Bash) > 0 && parsed.Accepted != "" && parsed.Accepted != emit {
			if strings.TrimSpace(parsed.DroppedPostBash) != "" {
				slog.Warn("loop: post-bash text dropped from assistant emit", "len", len(parsed.DroppedPostBash))
			}
			emit = parsed.Accepted
			replaceAssistantText(&assistantMsg, emit)
			if updateErr := deps.messages.Update(ctx, assistantMsg); updateErr != nil {
				slog.Warn("loop: persist sanitized assistant emit", "error", updateErr)
			}
		}
		// Check for empty emit.
		if strings.TrimSpace(emit) == "" {
			obs := rePromptEmpty()
			msgs = append(msgs,
				assistantTextMessage(emit, assistantMsg.ReasoningContent()),
				fantasy.NewUserMessage(obs),
			)
			if obsErr := persistObservation(ctx, deps, obs); obsErr != nil {
				slog.Warn("loop: persist empty re-prompt", "error", obsErr)
			}
			markStepFinished(ctx, deps, &assistantMsg, message.FinishReasonToolUse)
			msgs, _ = drainAndAppend(ctx, deps, msgs)
			continue
		}
		// Execute parsed run blocks: prose-only ends the turn,
		// but wait for active background jobs first so their
		// results are visible to the model before exit.
		if len(parsed.Bash) == 0 {
			var ended bool
			msgs, ended, err = tryEndTurn(ctx, deps, msgs, emit, &assistantMsg)
			if err != nil {
				return stopError, err
			}
			if !ended {
				continue
			}
			assistantMsg.AddFinish(message.FinishReasonEndTurn, "", "")
			if updateErr := deps.messages.Update(ctx, assistantMsg); updateErr != nil {
				slog.Warn("loop: persist text-only finish", "error", updateErr)
			}
			return stopEndTurn, nil
		}
		// Execute the single run block.
		bashCmd := parsed.Bash[0]
		if strings.TrimSpace(bashCmd) == "" {
			msgs, _ = drainAndAppend(ctx, deps, msgs)
			continue
		}
		if containsBlockedPattern(bashCmd) {
			obs := rePromptBlockedPattern()
			msgs = append(msgs,
				assistantTextMessage(emit, assistantMsg.ReasoningContent()),
				fantasy.NewUserMessage(obs),
			)
			if obsErr := persistObservation(ctx, deps, obs); obsErr != nil {
				slog.Warn("loop: persist banned re-prompt", "error", obsErr)
			}
			markStepFinished(ctx, deps, &assistantMsg, message.FinishReasonToolUse)
			msgs, _ = drainAndAppend(ctx, deps, msgs)
			continue
		}
		// Plain exit in a run block ends the turn, same as prose-only.
		if strings.TrimSpace(bashCmd) == lenosbash.ExitCommand {
			var ended bool
			msgs, ended, err = tryEndTurn(ctx, deps, msgs, emit, &assistantMsg)
			if err != nil {
				return stopError, err
			}
			if !ended {
				continue
			}
			assistantMsg.AddFinish(message.FinishReasonEndTurn, "", "")
			if updateErr := deps.messages.Update(ctx, assistantMsg); updateErr != nil {
				slog.Warn("loop: persist exit finish", "error", updateErr)
			}
			return stopEndTurn, nil
		}
		if cls, aux := classify(bashCmd); cls == classifyInvalidBash {
			obs := rePromptInvalidBash(aux)
			msgs = append(msgs,
				assistantTextMessage(emit, assistantMsg.ReasoningContent()),
				fantasy.NewUserMessage(obs),
			)
			if obsErr := persistObservation(ctx, deps, obs); obsErr != nil {
				slog.Warn("loop: persist invalid-bash re-prompt", "error", obsErr)
			}
			markStepFinished(ctx, deps, &assistantMsg, message.FinishReasonToolUse)
			msgs, _ = drainAndAppend(ctx, deps, msgs)
			continue
		}
		resultMsg, createErr := deps.messages.Create(ctx, deps.sessionID, message.CreateMessageParams{
			Role:  message.Result,
			Parts: []message.ContentPart{message.CommandContent{Command: bashCmd, Pending: true}},
		})
		if createErr != nil {
			return stopError, fmt.Errorf("create result row: %w", createErr)
		}
		res := deps.runner.Run(ctx, bashCmd, deps.env, deps.paths)
		if res.Background {
			exitBlock := lenosbash.BashBlock(lenosbash.ExitCommand)
			obs := fmt.Sprintf(
				"background job started (job_id: %s).\nYou can continue working while the job runs; use %s to end the turn when ready.",
				res.JobID, exitBlock,
			)
			obs = lenosbash.RuntimeBlock(obs)
			resultMsg.Parts = []message.ContentPart{message.CommandContent{
				Command: bashCmd, Pending: false, Observation: obs,
			}}
			if updateErr := deps.messages.Update(ctx, resultMsg); updateErr != nil {
				slog.Warn("loop: persist background job result row", "error", updateErr)
			}
			markStepFinished(ctx, deps, &assistantMsg, message.FinishReasonToolUse)
			msgs = append(msgs,
				assistantTextMessage(emit, assistantMsg.ReasoningContent()),
				fantasy.NewUserMessage(obs),
			)
			msgs, _ = drainAndAppend(ctx, deps, msgs)
			continue
		}
		if errors.Is(res.Err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			abandonPending(ctx, deps.messages, &resultMsg)
			return stopCanceled, ctx.Err()
		}
		exitCode := res.ExitCode
		stderr := string(res.Stderr)
		if res.Err != nil && len(res.Stdout) == 0 && stderr == "" {
			stderr = res.Err.Error()
		}
		envelope := formatResultForModel(bashCmd, string(res.Stdout), stderr, res.ExitCode)
		body := lenosbash.ResultBody(envelope)
		resultMsg.Parts = []message.ContentPart{message.CommandContent{
			Command:  bashCmd,
			Output:   string(combine(res.Stdout, []byte(stderr))),
			ExitCode: &exitCode, Pending: false,
			Observation: body,
		}}
		if updateErr := deps.messages.Update(ctx, resultMsg); updateErr != nil {
			slog.Warn("loop: persist result row", "error", updateErr)
		}
		markStepFinished(ctx, deps, &assistantMsg, message.FinishReasonToolUse)
		if errors.Is(res.Err, context.DeadlineExceeded) {
			exitCode := 124
			obs := rePromptTimeout(int(DefaultPerCmdTimeout / time.Second))
			resultMsg.Parts = []message.ContentPart{message.CommandContent{
				Command: bashCmd, Output: obs, ExitCode: &exitCode,
				Pending: false, Observation: obs,
			}}
			if updateErr := deps.messages.Update(ctx, resultMsg); updateErr != nil {
				slog.Warn("loop: persist timeout result row", "error", updateErr)
			}
			msgs = append(msgs,
				assistantTextMessage(emit, assistantMsg.ReasoningContent()),
				fantasy.NewUserMessage(obs),
			)
			msgs, _ = drainAndAppend(ctx, deps, msgs)
			continue
		}
		obs := lenosbash.ResultBlock(body)
		if firstNotFound := scanFirstCmdNotFound(stderr); firstNotFound != "" {
			rePrompt := rePromptCmdNotFound(firstNotFound)
			obs = rePrompt + "\n\n" + obs
			exitCode := 1
			resultMsg.Parts = []message.ContentPart{message.CommandContent{
				Command: bashCmd, Output: obs, ExitCode: &exitCode,
				Pending: false, Observation: obs,
			}}
			if updateErr := deps.messages.Update(ctx, resultMsg); updateErr != nil {
				slog.Warn("loop: persist cmd-not-found result row", "error", updateErr)
			}
		}
		msgs = append(msgs,
			assistantTextMessage(emit, assistantMsg.ReasoningContent()),
			fantasy.NewUserMessage(obs),
		)
		msgs, _ = drainAndAppend(ctx, deps, msgs)
	}
	return stopStepCap, ErrStepCap
}
