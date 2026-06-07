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

// waitForBackgroundJobs waits for any active background jobs to complete
// and returns whether any were active. It always calls WaitIdle (which is
// a no-op when ActiveCount is 0), avoiding the race between ActiveCount
// and WaitIdle.
func waitForBackgroundJobs(ctx context.Context, deps loopDeps) (bool, error) {
	if deps.bgRunner == nil {
		return false, nil
	}
	hadActive := deps.bgRunner.ActiveCount() > 0
	if err := deps.bgRunner.WaitIdle(ctx); err != nil {
		return false, err
	}
	return hadActive || deps.bgRunner.ActiveCount() > 0, nil
}

// tryEndTurn waits for background jobs and drains queued runtime prompts.
// It returns true if the loop should end (no outstanding work), or false
// if the loop should continue (background results or queued prompts were
// appended). On true, the caller must call finishEndTurn and return.
func tryEndTurn(ctx context.Context, deps loopDeps, msgs []fantasy.Message, emit string, assistantMsg *message.Message) ([]fantasy.Message, bool, error) {
	hadActive, err := waitForBackgroundJobs(ctx, deps)
	if err != nil {
		return msgs, false, err
	}
	markStepFinished(ctx, deps, assistantMsg, message.FinishReasonToolUse)
	msgs = append(msgs, assistantTextMessage(emit, assistantMsg.ReasoningContent()))
	var drained bool
	msgs, drained = drainAndAppend(ctx, deps, msgs)
	if hadActive || drained {
		return msgs, false, nil
	}
	return msgs, true, nil
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
		msgs = append(msgs, turnPromptMessage(prompt))
	}
	return msgs, true
}
