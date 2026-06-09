package agent

import (
	"context"
	"errors"
	"log/slog"

	"charm.land/fantasy"
	"github.com/tta-lab/lenos/internal/message"
)

func isCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
}

// tryEndTurn waits for background jobs to finish, ensures their
// completions are drained, and drains queued runtime prompts.
// It returns true if the loop should end (no outstanding work), or false
// if the loop should continue (background completions or queued prompts
// were appended). On true, the caller must call finishEndTurn and return.
func tryEndTurn(ctx context.Context, deps loopDeps, msgs []fantasy.Message, emit string, assistantMsg *message.Message) ([]fantasy.Message, bool, error) {
	// Wait for all background jobs to finish and drain their
	// completions. WaitAndDrain returns the formatted prompts so
	// they are visible to the model immediately — no race.
	var bgPrompts []turnPrompt
	if deps.bgRunner != nil {
		bgPrompts = deps.bgRunner.WaitAndDrain(ctx)
		if err := ctx.Err(); err != nil {
			return msgs, false, err
		}
	}

	// Drain queued runtime prompts (everything except background
	// completions, which we already have).
	queued, _ := drainQueue(deps)

	// Merge background completions and queued prompts.
	var continuePrompts []turnPrompt
	continuePrompts = append(continuePrompts, bgPrompts...)
	continuePrompts = append(continuePrompts, queued...)

	if len(continuePrompts) > 0 {
		// Only mark ToolUse when we actually continue.
		markStepFinished(ctx, deps, assistantMsg, message.FinishReasonToolUse)
		msgs = append(msgs, assistantTextMessage(emit, assistantMsg.ReasoningContent()))
		for _, p := range continuePrompts {
			if p.Persist {
				role := p.Role
				if role == "" {
					role = message.User
				}
				if _, err := deps.messages.Create(ctx, deps.sessionID, message.CreateMessageParams{
					Role:  role,
					Parts: []message.ContentPart{message.TextContent{Text: p.Text}},
				}); err != nil {
					slog.Warn("loop: persist continue prompt", "error", err)
				}
			}
			if err := recordTrajectoryPrompt(ctx, deps.trajectoryRecorder, p); err != nil {
				slog.Warn("trajectory: record continue prompt", "error", err)
			}
			msgs = append(msgs, turnPromptMessage(p))
		}
		return msgs, false, nil
	}

	// Check goal status before allowing natural exit.
	if deps.goalPath != "" {
		status, err := ReadGoalStatus(deps.goalPath)
		if err != nil {
			slog.Warn("loop: read goal status", "path", deps.goalPath, "error", err)
		}
		if status != GoalComplete && status != GoalBlocked {
			// Goal is still active — inject check hint and continue.
			hint := goalCheckHint()
			markStepFinished(ctx, deps, assistantMsg, message.FinishReasonToolUse)
			msgs = append(msgs, assistantTextMessage(emit, assistantMsg.ReasoningContent()))
			msgs = append(msgs, turnPromptMessage(turnPrompt{
				Text:    hint,
				Persist: true,
				Role:    message.Runtime,
			}))
			if _, err := deps.messages.Create(ctx, deps.sessionID, message.CreateMessageParams{
				Role:  message.Runtime,
				Parts: []message.ContentPart{message.TextContent{Text: hint}},
			}); err != nil {
				slog.Warn("loop: persist goal check hint", "error", err)
			}
			if err := recordTrajectoryPrompt(ctx, deps.trajectoryRecorder, turnPrompt{
				Text:    hint,
				Persist: true,
				Role:    message.Runtime,
			}); err != nil {
				slog.Warn("trajectory: record goal check hint", "error", err)
			}
			return msgs, false, nil
		}
	}

	// No background completions, no queued prompts — truly end.
	return msgs, true, nil
}

// drainQueue extracts queued turn prompts without appending them to the
// message stream.  The caller decides what to do with them.
func drainQueue(deps loopDeps) ([]turnPrompt, bool) {
	if deps.drainQueue == nil {
		return nil, false
	}
	drained := deps.drainQueue()
	return drained, len(drained) > 0
}

func drainAndAppend(ctx context.Context, deps loopDeps, msgs []fantasy.Message) ([]fantasy.Message, bool) {
	if deps.drainQueue == nil {
		return msgs, false
	}
	drained := deps.drainQueue()
	if len(drained) == 0 {
		return msgs, false
	}
	for _, prompt := range drained {
		if prompt.Persist {
			role := prompt.Role
			if role == "" {
				role = message.User
			}
			if _, err := deps.messages.Create(ctx, deps.sessionID, message.CreateMessageParams{
				Role:  role,
				Parts: []message.ContentPart{message.TextContent{Text: prompt.Text}},
			}); err != nil {
				slog.Warn("loop: persist drained prompt", "error", err)
			}
		}
		if err := recordTrajectoryPrompt(ctx, deps.trajectoryRecorder, prompt); err != nil {
			slog.Warn("trajectory: record drained prompt", "error", err)
		}
		msgs = append(msgs, turnPromptMessage(prompt))
	}
	return msgs, true
}
