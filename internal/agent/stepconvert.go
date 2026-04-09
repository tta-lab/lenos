package agent

import (
	"context"
	"strings"

	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/logos"
)

// stepsToMessages converts logos step messages to lenos messages and persists them.
func stepsToMessages(ctx context.Context, steps []logos.StepMessage, sessionID string, msgs message.Service) error {
	for _, step := range steps {
		switch step.Role {
		case logos.StepRoleAssistant:
			parts := []message.ContentPart{}
			if step.Reasoning != "" {
				parts = append(parts, message.ReasoningContent{
					Thinking:  step.Reasoning,
					Signature: step.ReasoningSignature,
				})
			}
			text := strings.TrimSpace(step.Content)
			if text != "" {
				parts = append(parts, message.TextContent{Text: text})
			}
			_, err := msgs.Create(ctx, sessionID, message.CreateMessageParams{
				Role:  message.Assistant,
				Parts: parts,
			})
			if err != nil {
				return err
			}

		case logos.StepRoleResult:
			_, err := msgs.Create(ctx, sessionID, message.CreateMessageParams{
				Role:  message.Result,
				Parts: []message.ContentPart{message.TextContent{Text: step.Content}},
			})
			if err != nil {
				return err
			}

		case logos.StepRoleUser:
			_, err := msgs.Create(ctx, sessionID, message.CreateMessageParams{
				Role:  message.User,
				Parts: []message.ContentPart{message.TextContent{Text: step.Content}},
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}
