package chat

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/tta-lab/lenos/internal/ui/list"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

// MdBlockKind classifies a transcript block for styling. Mirrors
// tui.BlockKind — re-declared here to avoid a chat → tui import.
type MdBlockKind int

const (
	MdBlockOther   MdBlockKind = iota // bash / output / trailer / runtime / prose
	MdBlockUserMsg                    // **λ** user message — gets the left bar
)

// MdBlockItem is a list.List item that renders one block of the session
// .md transcript. Blocks come from tui.SplitBlocks; the item is the bridge
// between that text-shaped output and the chat-list interactivity (focus,
// highlight, copy).
//
// Styling differentiates user-msg blocks from everything else — only the
// user msg gets the left-border bar; bash / output / trailer / runtime /
// prose blocks render flush so the eye distinguishes "what I said" from
// "what the agent did" at a glance.
type MdBlockItem struct {
	*highlightableMessageItem
	*focusableMessageItem

	id        string
	rawSource string
	rendered  string
	kind      MdBlockKind
	sty       *styles.Styles
}

// NewMdBlockItem wraps a transcript block. id should be stable across
// re-parses (use tui.Block.ID()). rawSource is what RawRender returns
// (used for clipboard copy — verbatim .md text). rendered is the styled
// display text shown in the chat list. kind drives which prefix style
// applies (user-msg gets the bar, everything else doesn't).
func NewMdBlockItem(sty *styles.Styles, id, rawSource, rendered string, kind MdBlockKind) *MdBlockItem {
	return &MdBlockItem{
		highlightableMessageItem: defaultHighlighter(sty),
		focusableMessageItem:     &focusableMessageItem{},
		id:                       id,
		rawSource:                rawSource,
		rendered:                 rendered,
		kind:                     kind,
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

// Render implements list.Item. User-msg blocks get the bordered bar
// (focus changes its weight); other blocks get plain padding so they
// read as "agent territory" — no per-line border noise.
func (i *MdBlockItem) Render(width int) string {
	body := i.RawRender(width)
	prefix := i.linePrefix()
	if prefix == "" {
		return body
	}
	lines := strings.Split(body, "\n")
	for n, line := range lines {
		lines[n] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// linePrefix returns the rendered SGR string to prepend on every line of
// the block, or "" to render flush. Per-kind:
//   - UserMsg: UserBlurred (faint bar) or UserFocused (solid bar) — the
//     terracotta vertical bar that marks turns the user owns.
//   - Other: "" — non-user blocks are pre-quoted with `> ` before Glamour
//     renders them (see model.blockquotePrefix), so Glamour's blockquote
//     style already supplies the indent + amber marker. Adding our own
//     prefix would stack two bars on top of each other.
func (i *MdBlockItem) linePrefix() string {
	if i.kind == MdBlockUserMsg {
		if i.focused {
			return i.sty.Chat.Message.UserFocused.Render()
		}
		return i.sty.Chat.Message.UserBlurred.Render()
	}
	return ""
}

// Compile-time interface checks.
var (
	_ MessageItem        = (*MdBlockItem)(nil)
	_ list.Focusable     = (*MdBlockItem)(nil)
	_ list.Highlightable = (*MdBlockItem)(nil)
)
