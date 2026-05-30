package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/ui/common"
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
func TestAssistantMessageItem_RenderColonPrefixAsBashPreview(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	content := ":status\n# Done\nsecond line"
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: content},
		},
	}, true).(*AssistantMessageItem)
	rendered := ansi.Strip(item.RawRender(80))
	assert.Contains(t, rendered, "$ :status")
	assert.Contains(t, rendered, ":status")
	assert.NotContains(t, rendered, "second line")
	assert.Equal(t, content, item.CopyText())
}
func TestAssistantMessageItem_RenderColonWordAsBashPreview(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: ":data\nsecond line"},
		},
	}, true)
	rendered := ansi.Strip(item.RawRender(80))
	assert.Contains(t, rendered, "$ :data")
	assert.NotContains(t, rendered, "second line")
}
func TestAssistantMessageItem_RenderMessageBlockAsMarkdown(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "Ready.", Kind: message.TextContentKindMessageBlock},
		},
	}, true)
	rendered := ansi.Strip(item.RawRender(80))
	assert.Contains(t, rendered, "Ready.")
	assert.NotContains(t, rendered, "$ Ready.")
}
func TestAssistantMessageItem_RenderMixedEmitShowsMarkdownAndCommand(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	content := "m#\"\nLet me read this file.\n\"#\n\ncat main.go"
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: content},
		},
	}, true)
	rendered := ansi.Strip(item.RawRender(80))
	assert.Contains(t, rendered, "Let me read this file.")
	assert.Contains(t, rendered, "cat main.go")
	assert.NotContains(t, rendered, "m#\"")
	assert.NotContains(t, rendered, "\"#")
}
func TestAssistantMessageItem_RenderMessageBlockUsesMarkdownRenderer(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	content := "m#\"\n# Ready\n\n- one\n\"#"
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: content},
		},
	}, true)
	want, err := common.MarkdownRenderer(&sty, cappedMessageWidth(80)).Render("# Ready\n\n- one")
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(ansi.Strip(want)), strings.TrimSpace(ansi.Strip(item.RawRender(80))))
}
func TestAssistantMessageItem_RenderMixedEmitMultilineBashOneLinePreview(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	content := "m\"Ready.\"\ncat main.go \\\n&& go test ./..."
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: content},
		},
	}, true)
	rendered := ansi.Strip(item.RawRender(80))
	assert.Contains(t, rendered, "Ready.")
	assert.Contains(t, rendered, "cat main.go")
	assert.NotContains(t, rendered, "&& go test")
}
func TestAssistantMessageItem_RenderMultipleMessageBlocksJoined(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	content := "m\"First.\"\nm\"Second.\""
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: content},
		},
	}, true)
	rendered := ansi.Strip(item.RawRender(80))
	assert.Contains(t, rendered, "First.")
	assert.Contains(t, rendered, "Second.")
	assert.NotContains(t, rendered, "m\"")
}
func TestAssistantMessageItem_RenderMessageBlockOnlyNoCommandPreview(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	content := "m####\"\nLong note.\n\"####"
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: content},
		},
	}, true)
	rendered := ansi.Strip(item.RawRender(80))
	assert.Contains(t, rendered, "Long note.")
	assert.NotContains(t, rendered, "$")
	assert.NotContains(t, rendered, "m#")
}
