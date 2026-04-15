package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

func TestExtractMessageItems_Assistant_EmptyContent(t *testing.T) {
	sty := styles.DefaultStyles()
	msg := &message.Message{
		ID:    "test-empty-assistant",
		Role:  message.Assistant,
		Parts: []message.ContentPart{message.TextContent{Text: ""}},
	}
	msg.AddFinish(message.FinishReasonEndTurn, "", "")

	items := ExtractMessageItems(&sty, msg, false)
	require.Len(t, items, 1, "empty-content assistant message must produce exactly one MessageItem")
	_, ok := items[0].(*AssistantMessageItem)
	assert.True(t, ok, "item must be an AssistantMessageItem")
}
