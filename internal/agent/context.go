package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/tta-lab/lenos/internal/agent/lenosbash"
	"github.com/tta-lab/lenos/internal/message"
)

// persistRuntimeContextCommands saves synthetic context commands as
// assistant+result message rows so they appear in history like real turns
// but without live model interaction.
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

// persistSyntheticCommandResult executes a RuntimeContextCommand via the
// runner and persists it as a synthetic assistant/result message pair.
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
	stderrBytes := res.Stderr
	if res.Err != nil && len(res.Stdout) == 0 && len(stderrBytes) == 0 {
		stderrBytes = []byte(res.Err.Error())
	}
	stderrStr := string(stderrBytes)
	// Synthetic context commands (e.g. --help) are reference material the
	// agent needs in full; never truncate them. Unlike user-run commands,
	// where tail-biased truncation is fine, help text must be complete.
	boundedStdout := boundOutput(res.Stdout, nil, call.DataDir)
	boundedStderr := boundOutput([]byte(stderrStr), nil, call.DataDir)
	envelope := formatResultForModel(commandForBash, boundedStdout.Preview, boundedStderr.Preview, res.ExitCode)
	body := lenosbash.ResultBody(envelope)
	outputStr := string(combine(res.Stdout, stderrBytes))
	if boundedStdout.FullPath != "" || boundedStderr.FullPath != "" {
		outputStr = body
	}
	resultMsg.Parts = []message.ContentPart{message.CommandContent{
		Command:     commandForBash,
		Output:      outputStr,
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

// persistSyntheticProse persists prose-only context commands
// (no bash block) as plain assistant messages.
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

// loopDepsForSynthetic returns minimal loopDeps for synthetic message
// operations that don't need a full runner.
func (a *sessionAgent) loopDepsForSynthetic(sessionID string) loopDeps {
	return loopDeps{
		messages:  a.messages,
		sessionID: sessionID,
	}
}
