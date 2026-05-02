package tui

import (
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	glamourstyles "charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
)

// GlyphLambda is the user-turn anchor. Kept as a const so call sites stay
// disciplined — never spell out the literal Greek letter inline.
const GlyphLambda = "λ"

// Accent colours used by the chat list and transcript renderer. Slimmed
// to the tokens that survived the internal/tui orphan cleanup — slate /
// crimson lived in the orphaned chrome modules and went with them.
var (
	AccentAmber = lipgloss.Color("214")     // #ffaf00 — phosphor amber, used for the λ user-turn glyph
	AccentBrass = lipgloss.Color("#b8973e") // antique gold — `$` shell-prompt prefix on lenos-bash composites
)

// BashPromptStyle paints the leading `$ ` on a lenos-bash composite block.
// Exported so the chat list can render it directly without going through a
// Styles aggregator (which used to exist for the orphaned tui chrome — see
// internal/tui/doc.go for the cleanup history).
var BashPromptStyle = lipgloss.NewStyle().Foreground(AccentBrass)

// MarkdownRenderer returns a Glamour TermRenderer with our theme overrides
// stacked on top of the default dark style — Glamour's WithStyles fully
// replaces the active StyleConfig, so passing only our overrides would
// strip default heading / bold / list styling and leave plain `## Foo`
// in the output.
//
// We override:
//   - Document.Margin: cleared (default is 2). The chat list owns the
//     left gutter via per-block linePrefix; an extra Glamour margin on
//     top would stack two indents and make focus toggles "shift" content
//     because the prefix-driven 2 chars combine with the margin-driven 2.
//   - BlockQuote: amber `>` marker
//   - CodeBlock / Code: terminal default fg/bg (we don't paint user code)
//
// Everything else (headings, bold/italic, lists, links, tables) keeps the
// stock dark-mode appearance.
func MarkdownRenderer(width int) (*glamour.TermRenderer, error) {
	cfg := glamourstyles.DarkStyleConfig
	cfg.Document = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockPrefix: "\n",
			BlockSuffix: "\n",
		},
		// Margin intentionally absent — the chat list's per-block prefix
		// system owns left alignment.
	}
	cfg.BlockQuote = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{Color: pointerTo("214")},
		Indent:         new(uint(1)),
		IndentToken:    new(">"),
	}
	cfg.CodeBlock = ansi.StyleCodeBlock{
		StyleBlock: ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{}},
	}
	cfg.Code = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{}}
	return glamour.NewTermRenderer(
		glamour.WithStyles(cfg),
		glamour.WithWordWrap(width),
	)
}

func pointerTo[T any](v T) *T { return &v }
