package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"github.com/tta-lab/temenos/client"

	"github.com/tta-lab/lenos/internal/message"
)

// StepCap bounds how many model emissions one Run() call can issue. Each
// emission counts (bash, log call, exit, empty/invalid emit alike); when the
// loop reaches the cap it halts with a runtime event and returns ErrStepCap.
const StepCap = 500

// ErrStepCap signals that the loop halted because the model issued StepCap
// emissions without hitting an `exit` (i.e. likely runaway).
var ErrStepCap = errors.New("agent: step cap reached")

// loopDeps wires the bash-first loop to its environment. Every field is
// required except onUsage, which may be nil (no per-step usage callback).
type loopDeps struct {
	model Model
	// drainQueue pulls queued user prompts off the session queue. Called at
	// every mid-loop step boundary so user followups ride the next model
	// request alongside the bash-result observation. nil-safe: drainAndAppend
	// treats nil as "no drain hook" and is a no-op.
	drainQueue func() []string
	provOpts   fantasy.ProviderOptions
	messages   message.Service
	runner     Runner
	salvage    bashSalvageProbe
	sessionID  string
	sysPrompt  string
	env        map[string]string
	paths      []client.AllowedPath
	// defaultNarrationTarget is applied to narrate calls without --to.
	// Explicit --to values remain unchanged.
	defaultNarrationTarget string
	// onUsage is called after each step with usage metrics.
	onUsage func(stepIdx int, u fantasy.Usage, m fantasy.ProviderMetadata)

	// shouldSummarizeBeforeStep is called immediately before a model stream
	// starts. Return true to stop at the pre-step boundary and let the caller
	// compact the session before re-entering.
	shouldSummarizeBeforeStep func(stepIdx int) bool

	// postStepHook is called after each step with the just-completed step's
	// usage. Nil-safe: a nil value is a no-op.
	postStepHook func(stepIdx int, u fantasy.Usage)

	// jobWatcher tracks background temenos jobs and enqueues completion
	// notifications. Nil when sandbox is not in use.
	jobWatcher *JobWatcher
}

// stopReason explains why runLoop returned. The caller maps it to the right
// follow-up: success queueing, error propagation, etc.
type stopReason int

const (
	stopExit            stopReason = iota // model emitted `exit`
	stopStepCap                           // 500 emissions without exit
	stopError                             // unrecoverable error (provider, persistence)
	stopCanceled                          // ctx canceled mid-stream or mid-exec
	stopShouldSummarize                   // pre-step callback requested auto-compact
)

// runLoop drives one turn of the bash-first agent: stream → classify → exec
// (or skip) → repeat until the model emits `exit` or the step cap fires.
// history is prior turns' fantasy messages; prompt is the just-arrived user
// input.
func runLoop(ctx context.Context, deps loopDeps, history []fantasy.Message, prompt string) (stopReason, error) {
	msgs := make([]fantasy.Message, 0, len(history)+2)
	msgs = append(msgs, fantasy.NewSystemMessage(deps.sysPrompt))
	msgs = append(msgs, history...)
	msgs = append(msgs, fantasy.NewUserMessage(prompt))

	for step := 0; step < StepCap; step++ {
		if deps.shouldSummarizeBeforeStep != nil && deps.shouldSummarizeBeforeStep(step) {
			return stopShouldSummarize, nil
		}

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
			deps.postStepHook(step, usage)
		}
		if deps.onUsage != nil {
			deps.onUsage(step, usage, meta)
		}

		probe := deps.salvage
		if probe == nil {
			probe = runnerBashSalvageProbe{
				runner: deps.runner,
				env:    deps.env,
				paths:  deps.paths,
			}
		}
		cls, aux := classifyWithSalvageProbe(ctx, emit, probe)
		if cls == classifyExec && aux != "" {
			emit = aux
			replaceAssistantText(&assistantMsg, emit)
			if updateErr := deps.messages.Update(ctx, assistantMsg); updateErr != nil {
				slog.Warn("loop: persist rewritten bash emit", "error", updateErr)
			}
		}
		if cls == classifyNaturalLanguage {
			emit = narrateCommandForBody(emit)
			replaceAssistantText(&assistantMsg, emit)
			if updateErr := deps.messages.Update(ctx, assistantMsg); updateErr != nil {
				slog.Warn("loop: persist natural-language narrate rewrite", "error", updateErr)
			}
			cls = classifyExec
		}

		switch cls {
		case classifyExit:
			assistantMsg.AddFinish(message.FinishReasonEndTurn, "", "")
			if updateErr := deps.messages.Update(ctx, assistantMsg); updateErr != nil {
				slog.Warn("loop: persist exit finish", "error", updateErr)
			}
			return stopExit, nil

		case classifyEmpty:
			obs := rePromptEmpty()
			msgs = append(msgs,
				assistantTextMessage(emit, assistantMsg.ReasoningContent()),
				fantasy.NewUserMessage(obs),
			)
			if obsErr := persistObservation(ctx, deps, obs); obsErr != nil {
				slog.Warn("loop: persist empty re-prompt", "error", obsErr)
			}
			markStepFinished(ctx, deps, &assistantMsg, message.FinishReasonToolUse)
			msgs = drainAndAppend(ctx, deps, msgs)

		case classifyToolCall:
			// Design choice: drop the assistant row for tool-call-shaped emits
			// instead of preserving it in DB/history. This is a pure format error,
			// not a content error: the model has not produced a valid bash action,
			// only a hallucinated wrapper schema. Replaying that wrapper teaches the
			// next turn to imitate it again. We do NOT apply this to branches like
			// invalid-bash, where seeing the exact broken shell text helps the model
			// repair quoting/syntax on the next turn. Safe in lenos because there is
			// no tool_use_id / tool-result pairing invariant to preserve. See
			// flicknote 4ddde3f1 for the regression analysis behind this asymmetry.
			obs := rePromptToolCall()
			if err := deps.messages.Delete(ctx, assistantMsg.ID); err != nil {
				slog.Warn("loop: delete tool-call assistant message", "error", err)
			}
			msgs = append(msgs, fantasy.NewUserMessage(obs))
			if obsErr := persistObservation(ctx, deps, obs); obsErr != nil {
				slog.Warn("loop: persist tool-call re-prompt", "error", obsErr)
			}
			msgs = drainAndAppend(ctx, deps, msgs)

		case classifyInvalidBash:
			obs := rePromptInvalidBash(aux)
			msgs = append(msgs,
				assistantTextMessage(emit, assistantMsg.ReasoningContent()),
				fantasy.NewUserMessage(obs),
			)
			if obsErr := persistObservation(ctx, deps, obs); obsErr != nil {
				slog.Warn("loop: persist invalid-bash re-prompt", "error", obsErr)
			}
			markStepFinished(ctx, deps, &assistantMsg, message.FinishReasonToolUse)
			msgs = drainAndAppend(ctx, deps, msgs)

		case classifyBanned:
			obs := rePromptBlockedPattern()
			msgs = append(msgs,
				assistantTextMessage(emit, assistantMsg.ReasoningContent()),
				fantasy.NewUserMessage(obs),
			)
			if obsErr := persistObservation(ctx, deps, obs); obsErr != nil {
				slog.Warn("loop: persist banned re-prompt", "error", obsErr)
			}
			markStepFinished(ctx, deps, &assistantMsg, message.FinishReasonToolUse)
			msgs = drainAndAppend(ctx, deps, msgs)

		case classifyExec:
			resultMsg, createErr := deps.messages.Create(ctx, deps.sessionID, message.CreateMessageParams{
				Role:  message.Result,
				Parts: []message.ContentPart{message.CommandContent{Command: emit, Pending: true}},
			})
			if createErr != nil {
				return stopError, fmt.Errorf("create result row: %w", createErr)
			}

			inv, invErr := newNarrateInvocation(emit, deps.env, deps.paths, deps.defaultNarrationTarget)
			if invErr != nil {
				abandonPending(ctx, deps.messages, &resultMsg)
				return stopError, fmt.Errorf("create narrate IPC directory: %w", invErr)
			}
			res := deps.runner.Run(ctx, inv.bash, inv.env, inv.paths)
			narrations, narrateErr := readNarrationEvents(inv.dir)
			inv.cleanup()
			if narrateErr != nil {
				abandonPending(ctx, deps.messages, &resultMsg)
				return stopError, fmt.Errorf("read narrate IPC events: %w", narrateErr)
			}

			// Background job: command exceeded the auto-background threshold
			// and was detached. Notify the model with <-Runtime format and
			// continue without blocking on the result.
			if res.Background {
				if deps.jobWatcher != nil {
					deps.jobWatcher.AddJob(res.JobID, emit)
				}
				obs := fmt.Sprintf(
					"<-Runtime background job started (job_id: %s)\nyou can check status or kill this job later via `temenos job kill %s`",
					res.JobID, res.JobID,
				)
				resultMsg.Parts = []message.ContentPart{message.CommandContent{
					Command:     emit,
					Pending:     false,
					Observation: obs,
				}}
				if updateErr := deps.messages.Update(ctx, resultMsg); updateErr != nil {
					slog.Warn("loop: persist background job result row", "error", updateErr)
				}
				markStepFinished(ctx, deps, &assistantMsg, message.FinishReasonToolUse)
				msgs = append(msgs,
					assistantTextMessage(emit, assistantMsg.ReasoningContent()),
					fantasy.NewUserMessage(obs),
				)
				msgs = drainAndAppend(ctx, deps, msgs)
				continue
			}

			// Honor mid-exec cancellation: a canceled context means the agent
			// loop is shutting down and we should not pretend the command finished.
			if errors.Is(res.Err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				abandonPending(ctx, deps.messages, &resultMsg)
				return stopCanceled, ctx.Err()
			}
			narrations, deliveryFailed := deliverNarrations(ctx, deps.runner, deps.env, deps.paths, narrations)

			exitCode := res.ExitCode
			stderr := string(res.Stderr)
			if res.Err != nil && len(res.Stdout) == 0 && stderr == "" {
				stderr = res.Err.Error()
			}
			envelope := formatResultForModel(emit, string(res.Stdout), stderr, res.ExitCode)
			body := strings.TrimPrefix(envelope, "<result>\n")
			body = strings.TrimSuffix(body, "\n</result>")
			body = appendNarrationObservation(body, narrations)
			resultMsg.Parts = []message.ContentPart{message.CommandContent{
				Command:     emit,
				Output:      string(combine(res.Stdout, []byte(stderr))),
				ExitCode:    &exitCode,
				Pending:     false,
				Observation: body,
				Narrations:  narrations,
			}}
			if updateErr := deps.messages.Update(ctx, resultMsg); updateErr != nil {
				slog.Warn("loop: persist result row", "error", updateErr)
			}

			markStepFinished(ctx, deps, &assistantMsg, message.FinishReasonToolUse)

			if errors.Is(res.Err, context.DeadlineExceeded) {
				exitCode := 124
				obs := rePromptTimeout(int(DefaultPerCmdTimeout / time.Second))
				resultMsg.Parts = []message.ContentPart{message.CommandContent{
					Command:     emit,
					Output:      obs,
					ExitCode:    &exitCode,
					Pending:     false,
					Observation: obs,
					Narrations:  narrations,
				}}
				if updateErr := deps.messages.Update(ctx, resultMsg); updateErr != nil {
					slog.Warn("loop: persist timeout result row", "error", updateErr)
				}
				msgs = append(msgs,
					assistantTextMessage(emit, assistantMsg.ReasoningContent()),
					fantasy.NewUserMessage(obs),
				)
				msgs = drainAndAppend(ctx, deps, msgs)
				continue
			}

			obs := "<result>\n" + body + "\n</result>"
			if firstNotFound := scanFirstCmdNotFound(stderr); firstNotFound != "" {
				rePrompt := rePromptCmdNotFound(firstNotFound)
				// SALIENCE FLIP: alert FIRST so the model sees the correction before the
				// (potentially success-looking) result envelope. Validated via worker session
				// d2f0a207: model reasoning ignored 20 trailing [runtime] re-prompts because
				// the envelope showed exit-0 with apparently-successful trailing command output.
				obs = rePrompt + "\n\n" + obs
				exitCode := 1

				// Keep the result row with non-zero exit code instead of abandoning it.
				resultMsg.Parts = []message.ContentPart{message.CommandContent{
					Command:     emit,
					Output:      obs,
					ExitCode:    &exitCode,
					Pending:     false,
					Observation: obs,
					Narrations:  narrations,
				}}
				if updateErr := deps.messages.Update(ctx, resultMsg); updateErr != nil {
					slog.Warn("loop: persist cmd-not-found result row", "error", updateErr)
				}
			}
			if shouldStopAfterNarration(narrations, res.ExitCode, deliveryFailed) {
				return stopExit, nil
			}
			msgs = append(msgs,
				assistantTextMessage(emit, assistantMsg.ReasoningContent()),
				fantasy.NewUserMessage(obs),
			)
			msgs = drainAndAppend(ctx, deps, msgs)
		}
	}

	return stopStepCap, ErrStepCap
}

func replaceAssistantText(msg *message.Message, text string) {
	parts := make([]message.ContentPart, 0, len(msg.Parts)+1)
	replaced := false
	for _, part := range msg.Parts {
		switch part.(type) {
		case message.TextContent:
			if !replaced {
				parts = append(parts, message.TextContent{Text: text})
				replaced = true
			}
		case message.Finish:
			continue
		default:
			parts = append(parts, part)
		}
	}
	if !replaced {
		parts = append(parts, message.TextContent{Text: text})
	}
	msg.Parts = parts
}

// streamOne pumps a single model stream into assistantMsg, returning the
// accumulated text emit and the final usage/metadata. Reasoning deltas are
// merged into assistantMsg.ReasoningContent; text deltas into TextContent.
// The assistant row is persisted incrementally so the UI sees live tokens.
func streamOne(
	ctx context.Context,
	deps loopDeps,
	msgs []fantasy.Message,
	assistantMsg *message.Message,
) (string, fantasy.Usage, fantasy.ProviderMetadata, error) {
	baseline := assistantMsg.Clone()
	result, err := retryModelStream(ctx,
		func() (streamOneResult, error) {
			return streamOneAttempt(ctx, deps, msgs, assistantMsg)
		},
		func() {
			resetMessageForStreamRetry(ctx, deps.messages, assistantMsg, baseline, "loop: reset assistant message for stream retry")
		},
	)
	if err != nil {
		return "", result.usage, result.meta, err
	}
	return result.emit, result.usage, result.meta, nil
}

type streamOneResult struct {
	emit  string
	usage fantasy.Usage
	meta  fantasy.ProviderMetadata
}

func streamOneAttempt(
	ctx context.Context,
	deps loopDeps,
	msgs []fantasy.Message,
	assistantMsg *message.Message,
) (streamOneResult, error) {
	stream, err := deps.model.Model.Stream(ctx, fantasy.Call{
		Prompt:          msgs,
		ProviderOptions: deps.provOpts,
		UserAgent:       userAgent,
	})
	if err != nil {
		return streamOneResult{}, err
	}

	var (
		usage fantasy.Usage
		meta  fantasy.ProviderMetadata
	)
	for part := range stream {
		switch part.Type {
		case fantasy.StreamPartTypeTextDelta:
			// First text delta finishes any in-progress reasoning span so
			// the UI splits "thinking" vs "saying" correctly (mirrors
			// agent_session.go Summarize behaviour).
			if rc := assistantMsg.ReasoningContent(); rc.Thinking != "" && rc.FinishedAt == 0 {
				assistantMsg.FinishThinking()
			}
			assistantMsg.AppendContent(part.Delta)
			if uerr := deps.messages.Update(ctx, *assistantMsg); uerr != nil {
				slog.Warn("loop: persist text delta", "error", uerr)
			}

		case fantasy.StreamPartTypeReasoningDelta:
			assistantMsg.AppendReasoningContent(part.Delta)
			if uerr := deps.messages.Update(ctx, *assistantMsg); uerr != nil {
				slog.Warn("loop: persist reasoning delta", "error", uerr)
			}

		case fantasy.StreamPartTypeReasoningEnd:
			if anthropicData, ok := part.ProviderMetadata[anthropic.Name]; ok {
				if sig, ok := anthropicData.(*anthropic.ReasoningOptionMetadata); ok && sig.Signature != "" {
					assistantMsg.AppendReasoningSignature(sig.Signature)
				}
			}
			if openaiData, ok := part.ProviderMetadata[openai.Name]; ok {
				if rd, ok := openaiData.(*openai.ResponsesReasoningMetadata); ok {
					assistantMsg.SetReasoningResponsesData(rd)
				}
			}
			if googleData, ok := part.ProviderMetadata[google.Name]; ok {
				if rd, ok := googleData.(*google.ReasoningMetadata); ok && rd.Signature != "" {
					assistantMsg.AppendThoughtSignature(rd.Signature, rd.ToolID)
				}
			}
			assistantMsg.FinishThinking()
			if uerr := deps.messages.Update(ctx, *assistantMsg); uerr != nil {
				slog.Warn("loop: persist reasoning end", "error", uerr)
			}

		case fantasy.StreamPartTypeFinish:
			usage = part.Usage
			meta = part.ProviderMetadata

		case fantasy.StreamPartTypeError:
			return streamOneResult{usage: usage, meta: meta}, part.Error
		}
	}

	return streamOneResult{
		emit:  assistantMsg.Content().Text,
		usage: usage,
		meta:  meta,
	}, nil
}

// formatResultForModel renders the next-turn observation text. Stdout and
// stderr are HTML-escaped so a literal `</result>` inside output cannot close
// the wrapper early. The `<result>...</result>` envelope is preserved so
// providers cached on older sessions don't re-train.
func formatResultForModel(_ string, stdout, stderr string, exitCode int) string {
	body := html.EscapeString(stdout)
	if stderr != "" {
		body += "\nSTDERR:\n" + html.EscapeString(stderr)
	}
	if body == "" {
		body = "Bash completed with no output"
	}
	if exitCode != 0 && exitCode != -1 {
		body += fmt.Sprintf("\n(exit code: %d)", exitCode)
	}
	return "<result>\n" + body + "\n</result>"
}

// assistantTextMessage builds the fantasy.Message we feed back into the next
// stream call to represent the model's just-emitted text. Keeps reasoning
// signatures in place so anthropic extended-thinking validation still passes.
func assistantTextMessage(text string, rc message.ReasoningContent) fantasy.Message {
	var parts []fantasy.MessagePart
	if rc.Thinking != "" {
		rp := fantasy.ReasoningPart{Text: rc.Thinking, ProviderOptions: fantasy.ProviderOptions{}}
		if rc.Signature != "" {
			rp.ProviderOptions[anthropic.Name] = &anthropic.ReasoningOptionMetadata{Signature: rc.Signature}
		}
		if rc.ResponsesData != nil {
			rp.ProviderOptions[openai.Name] = rc.ResponsesData
		}
		if rc.ThoughtSignature != "" {
			rp.ProviderOptions[google.Name] = &google.ReasoningMetadata{
				Signature: rc.ThoughtSignature,
				ToolID:    rc.ToolID,
			}
		}
		parts = append(parts, rp)
	}
	if t := strings.TrimSpace(text); t != "" {
		parts = append(parts, fantasy.TextPart{Text: t})
	}
	return fantasy.Message{Role: fantasy.MessageRoleAssistant, Content: parts}
}

// persistObservation writes a User-role message with the runtime re-prompt
// text. The next turn's history will replay this as the [runtime] guidance.
func persistObservation(ctx context.Context, deps loopDeps, obs string) error {
	_, err := deps.messages.Create(ctx, deps.sessionID, message.CreateMessageParams{
		Role:  message.Result,
		Parts: []message.ContentPart{message.TextContent{Text: obs}},
	})
	return err
}

// markStepFinished sets the assistant row's finish reason so the UI shows
// the step boundary. Errors are logged; persistence failures should not abort
// the loop.
func markStepFinished(ctx context.Context, deps loopDeps, msg *message.Message, reason message.FinishReason) {
	if msg.IsFinished() {
		return
	}
	msg.AddFinish(reason, "", "")
	if err := deps.messages.Update(ctx, *msg); err != nil {
		slog.Warn("loop: persist step finish", "error", err)
	}
}

// abandonPending marks a still-Pending Result row as canceled so it doesn't
// linger in the UI as a forever-spinning command.
func abandonPending(ctx context.Context, msgs message.Service, m *message.Message) {
	exitCode := -1
	cmd := ""
	if cc := m.CommandContent(); cc.Command != "" {
		cmd = cc.Command
	}
	m.Parts = []message.ContentPart{message.CommandContent{
		Command:  cmd,
		Output:   "canceled before result",
		ExitCode: &exitCode,
		Pending:  false,
	}}
	if err := msgs.Update(ctx, *m); err != nil {
		slog.Warn("loop: abandon pending result", "error", err)
	}
}

// combine concatenates stdout and stderr into a single byte slice, mirroring
// the temenos sandbox's merged-output convention. Stderr (if present) is
// joined with a newline so the model sees clean separation.
func combine(stdout, stderr []byte) []byte {
	if len(stderr) == 0 {
		return stdout
	}
	if len(stdout) == 0 {
		return stderr
	}
	var buf bytes.Buffer
	buf.Write(stdout)
	if !bytes.HasSuffix(stdout, []byte("\n")) {
		buf.WriteByte('\n')
	}
	buf.Write(stderr)
	return buf.Bytes()
}

// cmdNotFoundRe matches the bash diagnostic for an unknown command. Bash uses two
// formats depending on whether the emit was a single-line or multi-line script:
//   - single-line: "bash: <token>: command not found"
//   - multi-line:  "bash: line N: <token>: command not found"
//
// The multi-line format is dominant for fence-shape emits and heredoc failures.
// Anchored at line start so we capture the FIRST not-found token in multi-line
// stderr, matching bash's left-to-right execution order.
var cmdNotFoundRe = regexp.MustCompile(`(?m)^bash:(?: line \d+:)? (\S+): command not found$`)

// scanFirstCmdNotFound returns the first token bash reported as "command not found"
// in stderr, or "" if no match. Catches both:
//   - overall exit 127 (prose-only emit, stderr has the pattern)
//   - overall exit != 127 (missing command + trailing real command — bash runs
//     left to right, the missing command exits 127, but the trailing command's
//     exit masks it overall)
func scanFirstCmdNotFound(stderr string) string {
	m := cmdNotFoundRe.FindStringSubmatch(stderr)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// isCanceled reports whether err signals a cancellation (ctx.Done /
// context.Canceled). Used to map stream errors to stopCanceled vs stopError.
func isCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
}

// drainAndAppend pulls any queued user prompts off the session queue,
// persists each as a User-role message and appends them
// as separate fantasy.NewUserMessage entries to msgs.
//
// Called after the bash result / re-prompt observation has already been
// appended, so the model sees: bash result first, then user followups.
//
// Errors on persist / record are logged but never abort the loop.
func drainAndAppend(ctx context.Context, deps loopDeps, msgs []fantasy.Message) []fantasy.Message {
	if deps.drainQueue == nil {
		return msgs
	}
	drained := deps.drainQueue()
	for _, prompt := range drained {
		if _, err := deps.messages.Create(ctx, deps.sessionID, message.CreateMessageParams{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: prompt}},
		}); err != nil {
			slog.Warn("loop: persist drained user msg", "error", err)
		}
		msgs = append(msgs, fantasy.NewUserMessage(prompt))
	}
	return msgs
}
