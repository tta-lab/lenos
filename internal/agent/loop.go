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
	"github.com/tta-lab/lenos/internal/transcript"
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
	model fantasy.LanguageModel
	// drainQueue pulls queued user prompts off the session queue. Called at
	// every mid-loop step boundary so user followups ride the next model
	// request alongside the bash-result observation. nil-safe: drainAndAppend
	// treats nil as "no drain hook" and is a no-op.
	drainQueue func() []string
	provOpts   fantasy.ProviderOptions
	messages   message.Service
	runner     Runner
	recorder   transcript.Recorder
	sessionID  string
	sysPrompt  string
	providerID string // config provider ID (for assistant message Provider field)
	env        map[string]string
	paths      []client.AllowedPath
	// onUsage is called after each step with usage metrics.
	// Return true to request an early stop with stopShouldSummarize.
	onUsage func(stepIdx int, u fantasy.Usage, m fantasy.ProviderMetadata) bool
}

// stopReason explains why runLoop returned. The caller maps it to the right
// follow-up: success queueing, error propagation, etc.
type stopReason int

const (
	stopExit            stopReason = iota // model emitted `exit`
	stopStepCap                           // 500 emissions without exit
	stopError                             // unrecoverable error (provider, persistence)
	stopCanceled                          // ctx canceled mid-stream or mid-exec
	stopShouldSummarize                   // onUsage callback requested mid-turn auto-compact
)

// runLoop drives one turn of the bash-first agent: stream → classify → exec
// (or skip) → repeat until the model emits `exit` or the step cap fires.
// history is prior turns' fantasy messages; prompt is the just-arrived user
// input.
func runLoop(ctx context.Context, deps loopDeps, history []fantasy.Message, prompt string) (stopReason, error) {
	msgs := make([]fantasy.Message, 0, len(history)+2)
	msgs = append(msgs, fantasy.NewSystemMessage(deps.sysPrompt))
	msgs = append(msgs, history...)
	if err := deps.recorder.UserMessage(ctx, deps.sessionID, prompt); err != nil {
		slog.Warn("loop: record original user prompt", "error", err)
	}
	msgs = append(msgs, fantasy.NewUserMessage(prompt))

	for step := 0; step < StepCap; step++ {
		assistantMsg, err := deps.messages.Create(ctx, deps.sessionID, message.CreateMessageParams{
			Role:     message.Assistant,
			Parts:    []message.ContentPart{message.TextContent{Text: ""}},
			Model:    deps.model.Model(),
			Provider: deps.providerID,
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
		if deps.onUsage != nil {
			if deps.onUsage(step, usage, meta) {
				markStepFinished(ctx, deps, &assistantMsg, message.FinishReasonEndTurn)
				_ = deps.recorder.RuntimeEvent(ctx, deps.sessionID, transcript.SevWarn,
					"auto-compact: context window threshold reached; summarizing")
				_ = deps.recorder.TurnEnd(ctx, deps.sessionID)
				return stopShouldSummarize, nil
			}
		}

		// SSOT for emit visibility: record the model's emit BEFORE classification
		// so every emit reaches the .md transcript regardless of how it routes.
		tok, _ := deps.recorder.AgentEmit(ctx, deps.sessionID, emit)

		cls, bashErr := classify(ctx, emit)
		switch cls {
		case classifyExit:
			_ = deps.recorder.BashSkipped(ctx, tok, transcript.SevNormal, "exit — turn ends")
			_ = deps.recorder.TurnEnd(ctx, deps.sessionID)
			assistantMsg.AddFinish(message.FinishReasonEndTurn, "", "")
			if updateErr := deps.messages.Update(ctx, assistantMsg); updateErr != nil {
				slog.Warn("loop: persist exit finish", "error", updateErr)
			}
			return stopExit, nil

		case classifyEmpty:
			_ = deps.recorder.BashSkipped(ctx, tok, transcript.SevNormal, "empty emit; re-prompted")
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

		case classifyInvalidBash:
			_ = deps.recorder.BashSkipped(ctx, tok, transcript.SevWarn,
				fmt.Sprintf("invalid bash; bash -n said: %s; re-prompted", oneLine(bashErr)))
			obs := rePromptInvalidBash(bashErr)
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
			_ = deps.recorder.BashSkipped(ctx, tok, transcript.SevWarn, "blocked: sed -i / perl -i not allowed; use src edit")
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

		case classifyExec, classifyExecExit:
			resultMsg, createErr := deps.messages.Create(ctx, deps.sessionID, message.CreateMessageParams{
				Role:  message.Result,
				Parts: []message.ContentPart{message.CommandContent{Command: emit, Pending: true}},
			})
			if createErr != nil {
				return stopError, fmt.Errorf("create result row: %w", createErr)
			}

			res := deps.runner.Run(ctx, emit, deps.env, deps.paths)

			// Honor mid-exec cancellation BEFORE writing the recorder /
			// updating rows: a canceled context means the agent loop is
			// shutting down and we should not pretend the command finished.
			if errors.Is(res.Err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				_ = deps.recorder.BashSkipped(ctx, tok, transcript.SevWarn, "canceled")
				abandonPending(ctx, deps.messages, &resultMsg)
				return stopCanceled, ctx.Err()
			}

			_ = deps.recorder.BashResult(ctx, tok, combine(res.Stdout, res.Stderr), res.ExitCode, res.Duration)

			exitCode := res.ExitCode
			resultMsg.Parts = []message.ContentPart{message.CommandContent{
				Command:  emit,
				Output:   string(combine(res.Stdout, res.Stderr)),
				ExitCode: &exitCode,
				Pending:  false,
			}}
			if updateErr := deps.messages.Update(ctx, resultMsg); updateErr != nil {
				slog.Warn("loop: persist result row", "error", updateErr)
			}

			markStepFinished(ctx, deps, &assistantMsg, message.FinishReasonToolUse)

			if errors.Is(res.Err, context.DeadlineExceeded) {
				_ = deps.recorder.RuntimeEvent(ctx, deps.sessionID, transcript.SevWarn,
					"timeout after 120s; subprocess killed; partial output captured")
				obs := rePromptTimeout(int(DefaultPerCmdTimeout / time.Second))
				msgs = append(msgs,
					assistantTextMessage(emit, assistantMsg.ReasoningContent()),
					fantasy.NewUserMessage(obs),
				)
				if obsErr := persistObservation(ctx, deps, obs); obsErr != nil {
					slog.Warn("loop: persist timeout re-prompt", "error", obsErr)
				}
				msgs = drainAndAppend(ctx, deps, msgs)
				continue
			}

			// `cmd && exit` (and ; / ||): bash already executed both clauses,
			// the command succeeded, and the trailing exit signals turn-end
			// — no point continuing the loop just to re-prompt the model
			// for an empty next step.
			if cls == classifyExecExit {
				_ = deps.recorder.TurnEnd(ctx, deps.sessionID)
				assistantMsg.AddFinish(message.FinishReasonEndTurn, "", "")
				if updateErr := deps.messages.Update(ctx, assistantMsg); updateErr != nil {
					slog.Warn("loop: failed to persist message after exec-exit", "error", updateErr)
				}
				return stopExit, nil
			}

			stderr := string(res.Stderr)
			envelope := formatResultForModel(emit, string(res.Stdout), stderr, res.ExitCode)
			obs := envelope
			if firstNotFound := scanFirstCmdNotFound(stderr); firstNotFound != "" {
				_ = deps.recorder.RuntimeEvent(ctx, deps.sessionID, transcript.SevWarn,
					fmt.Sprintf("stderr matched 'command not found' on %q (exit %d); re-prompted",
						firstNotFound, res.ExitCode))
				rePrompt := rePromptCmdNotFound(firstNotFound)
				// SALIENCE FLIP: alert FIRST so the model sees the correction before the
				// (potentially success-looking) result envelope. Validated via worker session
				// d2f0a207: model reasoning ignored 20 trailing [runtime] re-prompts because
				// the envelope showed exit-0 with apparently-successful trailing command output.
				obs = rePrompt + "\n\n" + envelope
				if obsErr := persistObservation(ctx, deps, rePrompt); obsErr != nil {
					slog.Warn("loop: persist cmd-not-found re-prompt", "error", obsErr)
				}
			}
			msgs = append(msgs,
				assistantTextMessage(emit, assistantMsg.ReasoningContent()),
				fantasy.NewUserMessage(obs),
			)
			msgs = drainAndAppend(ctx, deps, msgs)
		}
	}

	_ = deps.recorder.RuntimeEvent(ctx, deps.sessionID, transcript.SevError,
		fmt.Sprintf("step cap (%d) reached; loop halted; awaiting owner", StepCap))
	return stopStepCap, ErrStepCap
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
	stream, err := deps.model.Stream(ctx, fantasy.Call{
		Prompt:          msgs,
		ProviderOptions: deps.provOpts,
		UserAgent:       userAgent,
	})
	if err != nil {
		return "", fantasy.Usage{}, nil, err
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
			return "", usage, meta, part.Error
		}
	}

	return assistantMsg.Content().Text, usage, meta, nil
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
		Role:  message.User,
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

// oneLine collapses bash -n's multi-line stderr into a single line for
// runtime-event descriptions (the full text is still in the re-prompt).
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:197] + "..."
	}
	return s
}

// cmdNotFoundRe matches the universal bash diagnostic "bash: <word>: command not found".
// Anchored at line start so we capture the FIRST not-found token in multi-line stderr,
// matching bash's left-to-right execution order — that is the offending prose-prefix.
var cmdNotFoundRe = regexp.MustCompile(`(?m)^bash: (\S+): command not found$`)

// scanFirstCmdNotFound returns the first token bash reported as "command not found"
// in stderr, or "" if no match. Catches both:
//   - overall exit 127 (prose-only emit, stderr has the pattern)
//   - overall exit != 127 (prose-prefix + trailing real command — bash runs left to
//     right, prose lines exit 127 but trailing command's exit masks them overall)
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
// persists each as a User-role message + transcript line, and appends them
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
		if err := deps.recorder.UserMessage(ctx, deps.sessionID, prompt); err != nil {
			slog.Warn("loop: record drained user msg", "error", err)
		}
		msgs = append(msgs, fantasy.NewUserMessage(prompt))
	}
	return msgs
}
