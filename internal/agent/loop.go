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
	model                     Model
	drainQueue                func() []turnPrompt
	provOpts                  fantasy.ProviderOptions
	pairWith                  string
	messages                  message.Service
	runner                    Runner
	sessionID                 string
	sysPrompt                 string
	env                       map[string]string
	paths                     []AllowedPath
	onUsage                   func(stepIdx int, u fantasy.Usage, m fantasy.ProviderMetadata)
	shouldSummarizeBeforeStep func(stepIdx int) bool
	postStepHook              func(stepIdx int, u fantasy.Usage)
	bgRunner                  *BackgroundRunner
}

// stopReason explains why runLoop returned.
type stopReason int

const (
	stopEndTurn stopReason = iota
	stopStepCap
	stopError
	stopCanceled
	stopShouldSummarize
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
		msgs = append(msgs, fantasy.NewUserMessage(prompt.Text))
	}
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
			msgs = drainAndAppend(ctx, deps, msgs)
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
			msgs = drainAndAppend(ctx, deps, msgs)
			continue
		}
		// Execute parsed run blocks: prose-only ends the turn.
		if len(parsed.Bash) == 0 {
			assistantMsg.AddFinish(message.FinishReasonEndTurn, "", "")
			if updateErr := deps.messages.Update(ctx, assistantMsg); updateErr != nil {
				slog.Warn("loop: persist text-only finish", "error", updateErr)
			}
			return stopEndTurn, nil
		}
		// Execute the single run block.
		bashCmd := parsed.Bash[0]
		if strings.TrimSpace(bashCmd) == "" {
			msgs = drainAndAppend(ctx, deps, msgs)
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
			msgs = drainAndAppend(ctx, deps, msgs)
			continue
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
			msgs = drainAndAppend(ctx, deps, msgs)
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
			obs := fmt.Sprintf(
				"background job started (job_id: %s)",
				res.JobID,
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
			msgs = drainAndAppend(ctx, deps, msgs)
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
			msgs = drainAndAppend(ctx, deps, msgs)
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
		msgs = drainAndAppend(ctx, deps, msgs)
	}
	return stopStepCap, ErrStepCap
}

// streamOne pumps a single model stream into assistantMsg.
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
	call := fantasy.Call{
		Prompt:          msgs,
		ProviderOptions: deps.provOpts,
		UserAgent:       userAgent,
	}
	stream, err := deps.model.Model.Stream(ctx, call)
	if err != nil {
		return streamOneResult{}, err
	}
	var (
		rawText strings.Builder
		usage   fantasy.Usage
		meta    fantasy.ProviderMetadata
	)
	for part := range stream {
		switch part.Type {
		case fantasy.StreamPartTypeTextDelta:
			rawText.WriteString(part.Delta)
			if rc := assistantMsg.ReasoningContent(); rc.Thinking != "" && rc.FinishedAt == 0 {
				assistantMsg.FinishThinking()
			}
			assistantMsg.AppendContent(part.Delta)
			trimAssistantPostBashTail(assistantMsg)
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
		emit:  rawText.String(),
		usage: usage,
		meta:  meta,
	}, nil
}

func trimAssistantPostBashTail(assistantMsg *message.Message) {
	parsed, diag := lenosbash.Parse(assistantMsg.Content().Text)
	if diag != nil || len(parsed.Bash) == 0 || parsed.Accepted == "" || parsed.Accepted == assistantMsg.Content().Text {
		return
	}
	replaceAssistantText(assistantMsg, parsed.Accepted)
}

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
	return lenosbash.ResultBlock(body)
}

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

func persistObservation(ctx context.Context, deps loopDeps, obs string) error {
	_, err := deps.messages.Create(ctx, deps.sessionID, message.CreateMessageParams{
		Role:  message.Result,
		Parts: []message.ContentPart{message.TextContent{Text: obs}},
	})
	return err
}

func markStepFinished(ctx context.Context, deps loopDeps, msg *message.Message, reason message.FinishReason) {
	if msg.IsFinished() {
		return
	}
	msg.AddFinish(reason, "", "")
	if err := deps.messages.Update(ctx, *msg); err != nil {
		slog.Warn("loop: persist step finish", "error", err)
	}
}

func abandonPending(ctx context.Context, msgs message.Service, m *message.Message) {
	exitCode := -1
	cmd := ""
	if cc := m.CommandContent(); cc.Command != "" {
		cmd = cc.Command
	}
	m.Parts = []message.ContentPart{message.CommandContent{
		Command: cmd, Output: "canceled before result", ExitCode: &exitCode, Pending: false,
	}}
	if err := msgs.Update(ctx, *m); err != nil {
		slog.Warn("loop: abandon pending result", "error", err)
	}
}

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

var cmdNotFoundRe = regexp.MustCompile(`(?m)^bash:(?: line \d+:)? (\S+): command not found$`)

func scanFirstCmdNotFound(stderr string) string {
	m := cmdNotFoundRe.FindStringSubmatch(stderr)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func isCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
}

func drainAndAppend(ctx context.Context, deps loopDeps, msgs []fantasy.Message) []fantasy.Message {
	if deps.drainQueue == nil {
		return msgs
	}
	drained := deps.drainQueue()
	for _, prompt := range drained {
		if prompt.Persist {
			if _, err := deps.messages.Create(ctx, deps.sessionID, message.CreateMessageParams{
				Role:  message.User,
				Parts: []message.ContentPart{message.TextContent{Text: prompt.Text}},
			}); err != nil {
				slog.Warn("loop: persist drained user msg", "error", err)
			}
		}
		msgs = append(msgs, fantasy.NewUserMessage(prompt.Text))
	}
	return msgs
}
