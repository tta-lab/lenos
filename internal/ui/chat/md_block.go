package chat

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/tta-lab/lenos/internal/ui/list"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

// MdBlockKind classifies a transcript block for styling. Mirrors
// transcript.BlockKind — re-declared here to keep the chat package
// independent of the transcript domain types (chat is a presentation
// layer; the blocks it renders happen to come from .md, but it doesn't
// need to import the parser).
type MdBlockKind int

const (
	MdBlockOther     MdBlockKind = iota // output / trailer / runtime / prose / legacy bash
	MdBlockUserMsg                      // **λ** user message — gets the left bar
	MdBlockLenosBash                    // ```lenos-bash composite — cmd + output rendered together
)

// MdBlockItem is a list.List item that renders one block of the session
// .md transcript. Blocks come from transcript.SplitBlocks; the item is the
// bridge between that text-shaped output and the chat-list interactivity
// (focus, highlight, copy).
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
// re-parses (use transcript.Block.ID()). rawSource is what RawRender returns
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

// RawRender implements list.RawRenderable. Returns the verbatim .md
// source so the chat-list copy path (y/c yank, mouse selection) yields
// transcript text without ANSI styling. Highlight wraps the raw source
// when set — clipboard extraction reads from this same path.
func (i *MdBlockItem) RawRender(width int) string {
	body := i.rawSource
	if i.isHighlighted() {
		body = i.renderHighlighted(body, width, lipgloss.Height(body))
	}
	return body
}

// Render implements list.Item. linePrefix is always non-empty (see
// linePrefix doc) so the gutter width never changes on focus toggle —
// only the visual identity of the prefix does.
func (i *MdBlockItem) Render(width int) string {
	body := i.rendered
	if i.isHighlighted() {
		body = i.renderHighlighted(body, width, lipgloss.Height(body))
	}
	prefix := i.linePrefix()
	lines := strings.Split(body, "\n")
	for n, line := range lines {
		lines[n] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// linePrefix returns the rendered SGR string to prepend on every line of
// the block. Always non-empty so the horizontal gutter is stable across
// focus toggles — toggling visibility (bar vs invisible) without changing
// width keeps content from shifting on j/k navigation. Per-kind:
//   - UserMsg: UserBlurred (faint bar) or UserFocused (solid bar) — the
//     terracotta vertical bar that marks turns the user owns. Always shown
//     so the eye can scan turn boundaries even when focus lives elsewhere.
//   - Other (bash / output / runtime / prose / lenos-bash composite):
//     BlockBlurred (transparent 2-char gutter) at rest, BlockFocused
//     (subtle slate bar in the same 2-char slot) when focused. Visual
//     change is the bar appearing, not the content moving.
func (i *MdBlockItem) linePrefix() string {
	if i.kind == MdBlockUserMsg {
		if i.focused {
			return i.sty.Chat.Message.UserFocused.Render()
		}
		return i.sty.Chat.Message.UserBlurred.Render()
	}
	if i.focused {
		return i.sty.Chat.Message.BlockFocused.Render()
	}
	return i.sty.Chat.Message.BlockBlurred.Render()
}

// IsStickyAnchor implements StickyAnchor — user-msg blocks pin to the
// top of the viewport when scrolled past so the reader always knows
// which turn they're inside.
func (i *MdBlockItem) IsStickyAnchor() bool { return i.kind == MdBlockUserMsg }

// StickyLine returns the first line of the rendered user-msg, used as the
// pinned header. The Glamour-rendered first line preserves the styled λ
// glyph; non-anchor blocks return "".
func (i *MdBlockItem) StickyLine() string {
	if i.kind != MdBlockUserMsg {
		return ""
	}
	if idx := strings.IndexByte(i.rendered, '\n'); idx >= 0 {
		return i.rendered[:idx]
	}
	return i.rendered
}

// StickyAnchor identifies items that may pin to the top of the chat
// viewport when scrolled past. Used for the sticky-lambda turn header.
type StickyAnchor interface {
	IsStickyAnchor() bool
	StickyLine() string
}

// Compile-time interface checks.
var (
	_ MessageItem        = (*MdBlockItem)(nil)
	_ list.Focusable     = (*MdBlockItem)(nil)
	_ list.Highlightable = (*MdBlockItem)(nil)
	_ StickyAnchor       = (*MdBlockItem)(nil)
)
