package chat

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/tta-lab/lenos/internal/ui/common"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

// NarrationMessageItem renders a command narration with the same treatment as
// assistant markdown prose.
type NarrationMessageItem struct {
	*highlightableMessageItem
	*cachedMessageItem
	*focusableMessageItem

	id   string
	body string
	sty  *styles.Styles
}

var _ MessageItem = (*NarrationMessageItem)(nil)

// NewNarrationMessageItem creates a new NarrationMessageItem.
func NewNarrationMessageItem(sty *styles.Styles, id string, body string) MessageItem {
	return &NarrationMessageItem{
		highlightableMessageItem: defaultHighlighter(sty),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     &focusableMessageItem{},
		id:                       id,
		body:                     body,
		sty:                      sty,
	}
}

// RawRender implements [MessageItem].
func (n *NarrationMessageItem) RawRender(width int) string {
	cappedWidth := cappedMessageWidth(width)

	content, height, ok := n.getCachedRender(cappedWidth)
	if ok {
		return n.renderHighlighted(content, cappedWidth, height)
	}

	body := strings.TrimSpace(n.body)
	renderer := common.MarkdownRenderer(n.sty, cappedWidth)
	rendered, err := renderer.Render(body)
	if err != nil {
		content = body
	} else {
		content = strings.TrimSuffix(rendered, "\n")
	}

	height = lipgloss.Height(content)
	n.setCachedRender(content, cappedWidth, height)
	return n.renderHighlighted(content, cappedWidth, height)
}

// Render implements [MessageItem].
func (n *NarrationMessageItem) Render(width int) string {
	return renderAssistantMessageLines(n.sty, n.focused, n.RawRender(width))
}

// ID implements [MessageItem].
func (n *NarrationMessageItem) ID() string {
	return n.id
}

// HandleKeyEvent implements [KeyEventHandler].
func (n *NarrationMessageItem) HandleKeyEvent(key tea.KeyMsg) (bool, tea.Cmd) {
	if k := key.String(); k == "c" || k == "y" {
		return true, common.CopyToClipboard(n.CopyText(), "Message copied to clipboard")
	}
	return false, nil
}

// CopyText implements [Copyable].
func (n *NarrationMessageItem) CopyText() string {
	return n.body
}
