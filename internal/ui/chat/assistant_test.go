package chat

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

func TestAssistantMessageItem_RenderBashEmitBeforeFinish(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "# checking repo state"},
		},
	}, true)

	rendered := ansi.Strip(item.RawRender(80))

	assert.Contains(t, rendered, "$ # checking repo state")
}

func TestAssistantMessageItem_RenderBashEmitPreviewIsOneLine(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "echo one\necho two"},
		},
	}, true).(*AssistantMessageItem)

	rendered := ansi.Strip(item.RawRender(80))

	assert.NotContains(t, rendered, "\n")
	assert.Contains(t, rendered, "$ echo one")
	assert.NotContains(t, rendered, "echo two")
	assert.Equal(t, "echo one\necho two", item.CopyText())
}

func TestAssistantMessageItem_RenderMdAsMarkdownKeepsProtocolText(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	content := ":md\n# Done\nsecond line"
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: content},
		},
	}, true).(*AssistantMessageItem)

	rendered := ansi.Strip(item.RawRender(80))

	assert.NotContains(t, rendered, "$ :md")
	assert.Contains(t, rendered, ":md")
	assert.Contains(t, rendered, "Done")
	assert.Contains(t, rendered, "second line")
	assert.Equal(t, content, item.CopyText())
}

func TestAssistantMessageItem_RenderColonNonMdAsBashPreview(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: ":mdata\nsecond line"},
		},
	}, true)

	rendered := ansi.Strip(item.RawRender(80))

	assert.Contains(t, rendered, "$ :mdata")
	assert.NotContains(t, rendered, "second line")
}
