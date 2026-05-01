package chat

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/tta-lab/lenos/internal/ui/list"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

// MdBlockItem is a list.List item that renders one block of the session
// .md transcript. Blocks come from tui.SplitBlocks; the item is the bridge
// between that text-shaped output and the chat-list interactivity (focus,
// highlight, copy).
//
// Rendered text is supplied by the caller (typically pre-rendered through
// Glamour at SetMessages time). MdBlockItem stays presentation-neutral —
// it just adds the focus prefix gutter, highlight overlay, and copy
// semantics.
type MdBlockItem struct {
	*highlightableMessageItem
	*focusableMessageItem

	id        string
	rawSource string
	rendered  string
	sty       *styles.Styles
}

// NewMdBlockItem wraps a transcript block. id should be stable across
// re-parses (use tui.Block.ID()). rawSource is what RawRender returns
// (used for clipboard copy — verbatim .md text). rendered is the styled
// display text shown in the chat list.
func NewMdBlockItem(sty *styles.Styles, id, rawSource, rendered string) *MdBlockItem {
	return &MdBlockItem{
		highlightableMessageItem: defaultHighlighter(sty),
		focusableMessageItem:     &focusableMessageItem{},
		id:                       id,
		rawSource:                rawSource,
		rendered:                 rendered,
		sty:                      sty,
	}
}

// ID implements chat.Identifiable.
func (i *MdBlockItem) ID() string { return i.id }

// RawRender implements list.RawRenderable. Returns the unstyled .md source
// so the chat-list copy path produces verbatim transcript text.
func (i *MdBlockItem) RawRender(width int) string {
	_ = width
	body := i.rendered
	h := lipgloss.Height(body)
	if i.isHighlighted() {
		body = i.renderHighlighted(body, width, h)
	}
	return body
}

// Render implements list.Item — applies the focus gutter prefix on each
// line so the user sees which block is selected.
func (i *MdBlockItem) Render(width int) string {
	body := i.RawRender(width)

	var prefix string
	if i.focused {
		prefix = i.sty.Chat.Message.UserFocused.Render()
	} else {
		prefix = i.sty.Chat.Message.UserBlurred.Render()
	}
	lines := strings.Split(body, "\n")
	for n, line := range lines {
		lines[n] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// Compile-time interface checks.
var (
	_ MessageItem        = (*MdBlockItem)(nil)
	_ list.Focusable     = (*MdBlockItem)(nil)
	_ list.Highlightable = (*MdBlockItem)(nil)
)
