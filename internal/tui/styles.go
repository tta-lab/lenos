package tui

import (
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
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
	AccentAmber   = lipgloss.Color("214") // #ffaf00 — phosphor amber
	AccentSlate   = lipgloss.Color("245") // #8a8a8a — dim chrome
	AccentCrimson = lipgloss.Color("160") // #d70000 — error/halt (reserved v2)
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
	}
}

// MarkdownRenderer returns a Glamour TermRenderer with our theme overrides.
// The theme:
//   - Block quote: amber foreground for the > marker, italic body unchanged
//   - Code block: terminal default fg/bg (we do not paint user code)
//   - Headings: bold; in our format h1/h2 do not appear
//   - Italic + bold: terminal default attrs
func MarkdownRenderer(width int) (*glamour.TermRenderer, error) {
	return glamour.NewTermRenderer(
		glamour.WithStyles(ansi.StyleConfig{
			BlockQuote: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: pointerTo("214"),
				},
				Indent:      new(uint(1)),
				IndentToken: new(">"),
			},
			CodeBlock: ansi.StyleCodeBlock{
				StyleBlock: ansi.StyleBlock{
					StylePrimitive: ansi.StylePrimitive{},
				},
			},
			Code: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{},
			},
		}),
		glamour.WithWordWrap(width),
	)
}

func pointerTo[T any](v T) *T { return &v }
