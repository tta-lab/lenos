package agent

import (
	"context"

	"charm.land/fantasy"

	"github.com/tta-lab/lenos/internal/message"
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

func getLastAssistantID(ctx context.Context, msgs message.Service, sessionID string) string {
	all, err := msgs.List(ctx, sessionID)
	if err != nil {
		return ""
	}
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Role == message.Assistant {
			return all[i].ID
		}
	}
	return ""
}
