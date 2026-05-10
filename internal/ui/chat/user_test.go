package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

func TestUserMessageItem_RenderUsesLambdaStyle(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	item := NewUserMessageItem(&sty, &message.Message{
		ID:   "user-1",
		Role: message.User,
		Parts: []message.ContentPart{
			message.TextContent{Text: "hello"},
		},
	}, nil)

	rendered := item.Render(80)
	lambda := sty.Chat.Message.UserLambda.Render("λ")

	require.NotEqual(t, "λ", lambda)
	assert.Contains(t, rendered, lambda+"  ")
}
