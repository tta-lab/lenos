package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tta-lab/lenos/internal/agent/lenosbash"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/ui/common"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

func TestAssistantMessageItem_RenderMarkdownBeforeFinish(t *testing.T) {
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
	assert.Contains(t, rendered, "checking repo state")
	assert.NotContains(t, rendered, "$ # checking repo state")
}

func TestAssistantMessageItem_RenderMarkdownProse(t *testing.T) {
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
	assert.NotContains(t, rendered, "$ echo one")
	assert.Contains(t, rendered, "echo one")
	assert.Contains(t, rendered, "echo two")
	assert.Equal(t, "echo one\necho two", item.CopyText())
}

func TestAssistantMessageItem_RenderColonPrefixAsMarkdown(t *testing.T) {
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
	assert.NotContains(t, rendered, "$ :status")
	assert.Contains(t, rendered, ":status")
	assert.Contains(t, rendered, "second line")
	assert.Equal(t, content, item.CopyText())
}

func TestAssistantMessageItem_RenderColonWordAsMarkdown(t *testing.T) {
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
	assert.NotContains(t, rendered, "$ :data")
	assert.Contains(t, rendered, ":data")
	assert.Contains(t, rendered, "second line")
}

func TestAssistantMessageItem_RenderProseAsMarkdown(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "Ready."},
		},
	}, true)
	rendered := ansi.Strip(item.RawRender(80))
	assert.Contains(t, rendered, "Ready.")
}

func TestAssistantMessageItem_RenderMixedEmitShowsMarkdownAndCommand(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	content := "Let me read this file.\n\n" + lenosbash.BashBlock("cat main.go")
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
	assert.NotContains(t, rendered, lenosbash.BashStartTag)
	assert.NotContains(t, rendered, lenosbash.BashEndTag)
}

func TestAssistantMessageItem_DropsPostBashText(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	content := "Let me read this file.\n\n" + lenosbash.BashBlock("cat main.go") + "\nThis should not render."
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
	assert.NotContains(t, rendered, "This should not render.")
}

func TestAssistantMessageItem_RenderCommandPrefixUsesCommandPrefixStyle(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	content := "Ready.\n" + lenosbash.BashBlock("cat main.go")
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: content},
		},
	}, true)

	rendered := item.RawRender(80)

	assert.Contains(t, rendered, sty.Chat.Message.CommandPrefix.Render("$"))
}

func TestAssistantMessageItem_RenderProseUsesMarkdownRenderer(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	content := "# Ready\n\n- one"
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
	content := "Ready.\n" + lenosbash.BashBlock("cat main.go \\\n&& go test ./...")
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

func TestAssistantMessageItem_RenderMultipleProseParagraphsJoined(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	content := "First.\n\nSecond."
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
}

func TestAssistantMessageItem_RenderProseOnlyNoCommandPreview(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	content := "Long note."
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
	assert.NotContains(t, rendered, lenosbash.BashStartTag)
}

func TestAssistantMessageItem_StreamingProseBeforeBashRendersMarkdown(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	// Simulate streaming: prose + unclosed bash block
	content := "Let me check the files.\n\n" + lenosbash.BashStartTag + "\nls -la\n"
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: content},
		},
	}, true)
	rendered := ansi.Strip(item.RawRender(80))
	assert.Contains(t, rendered, "Let me check the files.")
	assert.Contains(t, rendered, "ls -la")
	assert.NotContains(t, rendered, lenosbash.BashStartTag)
}

func TestAssistantMessageItem_StreamingUnclosedBashShowsCommandPreview(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	// Streaming: bash block opened, command partially received
	content := lenosbash.BashStartTag + "\ncat /etc/hosts\n"
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: content},
		},
	}, true)
	rendered := ansi.Strip(item.RawRender(80))
	assert.Contains(t, rendered, "cat /etc/hosts")
	assert.NotContains(t, rendered, lenosbash.BashStartTag)
}

func TestAssistantMessageItem_StreamingClosedBashDropsPostBash(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	// Complete bash block + trailing post-bash text
	content := "Before.\n" + lenosbash.BashBlock("cat main.go") + "\nExtra text."
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: content},
		},
	}, true)
	rendered := ansi.Strip(item.RawRender(80))
	assert.Contains(t, rendered, "Before.")
	assert.Contains(t, rendered, "cat main.go")
	assert.NotContains(t, rendered, "Extra text.")
}

func TestAssistantMessageItem_StreamingOnlyBashBlockOpened(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	// Just started opening a bash block — nothing in it yet
	content := "Let's run this.\n" + lenosbash.BashStartTag + "\n"
	item := NewAssistantMessageItem(&sty, &message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: content},
		},
	}, true)
	rendered := ansi.Strip(item.RawRender(80))
	assert.Contains(t, rendered, "Let's run this.")
	assert.NotContains(t, rendered, lenosbash.BashStartTag)
}
