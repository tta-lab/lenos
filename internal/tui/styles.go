package tui

import (
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	glamourstyles "charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
)

// Glyph constants for disciplined use throughout the TUI.
const (
	GlyphLambda       = "λ"
	GlyphArrowDown    = "↓"
	GlyphArrowUp      = "↑"
	GlyphArrowEndDown = "↡"
)

// Accent colours — three semantic tokens per orientation.
var (
	AccentAmber   = lipgloss.Color("214")     // #ffaf00 — phosphor amber
	AccentSlate   = lipgloss.Color("245")     // #8a8a8a — dim chrome
	AccentCrimson = lipgloss.Color("160")     // #d70000 — error/halt (reserved v2)
	AccentBrass   = lipgloss.Color("#b8973e") // antique gold — shell prompt, classic-terminal evocation
)

// Styles holds resolved lipgloss styles for TUI chrome elements.
type Styles struct {
	Header          lipgloss.Style // dim slate
	HeaderSep       lipgloss.Style // dim slate ─
	StickyLambda    lipgloss.Style // amber λ prefix + default body
	StickyTurnRight lipgloss.Style // dim slate turn N ↑
	StickySep       lipgloss.Style // dim slate ─
	NewIndicator    lipgloss.Style // dim amber ↓ N new ↡
	FooterActive    lipgloss.Style // amber text
	FooterIdle      lipgloss.Style // slate text
	FooterHints     lipgloss.Style // dim slate (right-aligned)
	SandboxOn       lipgloss.Style // green
	SandboxOff      lipgloss.Style // red
	SandboxDegraded lipgloss.Style // amber
	Brand           lipgloss.Style // bold cyan
	Keystroke       lipgloss.Style // dim
	KeystrokeTip    lipgloss.Style // dim slate
	WatchErr        lipgloss.Style // bold crimson — recoverable watch error
	BashPrompt      lipgloss.Style // cyan $ prefix on lenos-bash composite blocks
}

// NewStyles returns the resolved style set.
func NewStyles() Styles {
	return Styles{
		Header: lipgloss.NewStyle().
			Foreground(AccentSlate),

		HeaderSep: lipgloss.NewStyle().
			Foreground(AccentSlate),

		StickyLambda: lipgloss.NewStyle().
			Foreground(AccentAmber),

		StickyTurnRight: lipgloss.NewStyle().
			Foreground(AccentSlate),

		StickySep: lipgloss.NewStyle().
			Foreground(AccentSlate),

		NewIndicator: lipgloss.NewStyle().
			Foreground(AccentAmber),

		FooterActive: lipgloss.NewStyle().
			Foreground(AccentAmber),

		FooterIdle: lipgloss.NewStyle().
			Foreground(AccentSlate),

		FooterHints: lipgloss.NewStyle().
			Foreground(AccentSlate),

		SandboxOn: lipgloss.NewStyle().
			Foreground(lipgloss.Color("2")).
			Bold(true),

		SandboxOff: lipgloss.NewStyle().
			Foreground(lipgloss.Color("1")).
			Bold(true),

		SandboxDegraded: lipgloss.NewStyle().
			Foreground(lipgloss.Color("3")).
			Bold(true),

		Brand: lipgloss.NewStyle().
			Foreground(lipgloss.Color("6")).
			Bold(true),

		Keystroke: lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")),

		KeystrokeTip: lipgloss.NewStyle().
			Foreground(AccentSlate),

		WatchErr: lipgloss.NewStyle().
			Foreground(AccentCrimson).
			Bold(true),

		BashPrompt: lipgloss.NewStyle().
			Foreground(AccentBrass),
	}
}

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
