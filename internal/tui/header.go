package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Header renders the 1-row session status line.
type Header struct {
	fm        Frontmatter
	turnCount int
	width     int
	styles    Styles
}

// NewHeader creates a header with the given frontmatter and styles.
func NewHeader(fm Frontmatter, styles Styles) *Header {
	return &Header{
		fm:     fm,
		width:  100,
		styles: styles,
	}
}

// SetWidth updates the terminal width.
func (h *Header) SetWidth(w int) {
	h.width = w
}

// SetTurnCount updates the turn count.
func (h *Header) SetTurnCount(n int) {
	h.turnCount = n
}

// Render returns the 1-row header string.
func (h *Header) Render() string {
	turns := "turns"
	if h.turnCount == 1 {
		turns = "turn"
	}

	shortID := h.fm.SessionID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	var text string
	if h.width < 60 {
		text = h.fm.Agent + " · " + shortID
	} else {
		text = h.fm.Agent + " · " + shortID + " · " + h.fm.Model + " · " +
			fmt.Sprintf("%d", h.turnCount) + " " + turns
	}

	// Truncate from right if needed.
	if lipgloss.Width(text) > h.width {
		for lipgloss.Width(text) > h.width-1 && len(text) > 0 {
			text = text[:len(text)-1]
		}
		text = text[:len(text)-1] + "…"
	}

	return h.styles.Header.Render(text)
}

// RenderSep returns the separator line below the header.
func (h *Header) RenderSep() string {
	sep := strings.Repeat("─", h.width)
	return h.styles.HeaderSep.Render(sep)
}
