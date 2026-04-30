package tui

import (
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// Viewport holds the rendered transcript and the user's scroll position.
type Viewport struct {
	rendered      Rendered
	pinned        bool
	offset        int
	height        int
	width         int
	newSinceUnpin int
	styles        Styles
}

// NewViewport creates a viewport with the given dimensions and styles.
func NewViewport(width, height int, styles Styles) *Viewport {
	return &Viewport{
		height: height,
		width:  width,
		styles: styles,
		pinned: true,
	}
}

// SetRendered updates the buffer. If pinned, scrolls to bottom.
// If !pinned, increments newSinceUnpin by the new line count.
func (v *Viewport) SetRendered(r Rendered) {
	if v.pinned {
		v.rendered = r
		// Scroll to bottom.
		v.offset = max(0, len(r.Lines)-v.height)
		v.newSinceUnpin = 0
	} else {
		oldLines := len(v.rendered.Lines)
		v.rendered = r
		newLines := len(r.Lines)
		if newLines > oldLines {
			v.newSinceUnpin += newLines - oldLines
		}
	}
}

// SetSize updates the viewport dimensions.
func (v *Viewport) SetSize(w, h int) {
	v.width = w
	v.height = h
	if v.pinned && len(v.rendered.Lines) > 0 {
		// Adjust offset to keep bottom in view.
		v.offset = max(0, len(v.rendered.Lines)-h)
	}
}

// ScrollUp scrolls the viewport up by 1 line.
func (v *Viewport) ScrollUp() {
	if v.offset > 0 {
		v.offset--
	}
	v.pinned = false
}

// ScrollDown scrolls the viewport down by 1 line.
func (v *Viewport) ScrollDown() {
	maxOffset := max(0, len(v.rendered.Lines)-v.height)
	if v.offset < maxOffset {
		v.offset++
		if v.offset >= maxOffset {
			v.pinned = true
			v.newSinceUnpin = 0
		}
	} else {
		v.pinned = true
	}
}

// HalfPageUp scrolls up by half a page.
func (v *Viewport) HalfPageUp() {
	v.offset = max(0, v.offset-v.height/2)
	v.pinned = false
}

// HalfPageDown scrolls down by half a page.
func (v *Viewport) HalfPageDown() {
	maxOffset := max(0, len(v.rendered.Lines)-v.height)
	v.offset = min(maxOffset, v.offset+v.height/2)
	if v.offset >= maxOffset {
		v.pinned = true
		v.newSinceUnpin = 0
	}
}

// PageUp scrolls up by a full page.
func (v *Viewport) PageUp() {
	v.offset = max(0, v.offset-v.height)
	v.pinned = false
}

// PageDown scrolls down by a full page.
func (v *Viewport) PageDown() {
	maxOffset := max(0, len(v.rendered.Lines)-v.height)
	v.offset = min(maxOffset, v.offset+v.height)
	if v.offset >= maxOffset {
		v.pinned = true
		v.newSinceUnpin = 0
	}
}

// Home scrolls to the top.
func (v *Viewport) Home() {
	v.offset = 0
	v.pinned = false
}

// End scrolls to the bottom.
func (v *Viewport) End() {
	v.offset = max(0, len(v.rendered.Lines)-v.height)
	v.pinned = true
	v.newSinceUnpin = 0
}

// IsPinned returns whether the viewport is at the bottom.
func (v *Viewport) IsPinned() bool {
	return v.pinned
}

// Offset returns the current scroll offset (top line index).
func (v *Viewport) Offset() int {
	return v.offset
}

// stickyTurn returns the TurnAnchor that should be shown as the sticky marker,
// or nil if no sticky should be shown.
func (v *Viewport) stickyTurn() *TurnAnchor {
	if len(v.rendered.Anchors) == 0 || v.offset == 0 {
		return nil
	}
	// Binary search for the last anchor with StartLine <= offset.
	i := sort.Search(len(v.rendered.Anchors), func(i int) bool {
		return v.rendered.Anchors[i].StartLine > v.offset
	})
	if i > 0 && v.rendered.Anchors[i-1].StartLine < v.offset {
		return &v.rendered.Anchors[i-1]
	}
	return nil
}

// Render returns the full viewport content with sticky-λ and new indicator.
func (v *Viewport) Render() string {
	var lines []string
	contentHeight := v.height

	// Check for sticky turn marker.
	sticky := v.stickyTurn()
	if sticky != nil {
		contentHeight -= 2 // sticky row + separator
	}

	// Render content lines.
	endOffset := min(v.offset+contentHeight, len(v.rendered.Lines))
	for i := v.offset; i < endOffset && i < len(v.rendered.Lines); i++ {
		lines = append(lines, v.rendered.Lines[i])
	}

	// Build the output.
	var out strings.Builder

	if sticky != nil {
		// Render sticky λ heading.
		headerText := sticky.HeaderText
		if len(headerText) > v.width-10 {
			headerText = headerText[:v.width-13] + "…"
		}
		turnLabel := "turn " + strconv.Itoa(sticky.Number) + " " + GlyphArrowUp
		right := v.styles.StickyTurnRight.Render(turnLabel)
		padding := strings.Repeat(" ", max(0, v.width-lipgloss.Width(headerText)-lipgloss.Width(turnLabel)))
		out.WriteString(v.styles.StickyLambda.Render(GlyphLambda + " " + headerText))
		out.WriteString(padding)
		out.WriteString(right)
		out.WriteString("\n")

		// Separator line.
		out.WriteString(v.styles.StickySep.Render(strings.Repeat("─", v.width)))
		out.WriteString("\n")
	}

	// Content lines.
	for _, line := range lines {
		out.WriteString(line)
		out.WriteString("\n")
	}

	// New indicator.
	if !v.pinned && v.newSinceUnpin > 0 {
		// Fill to right with spaces, then write indicator.
		indicator := GlyphArrowDown + " " + strconv.Itoa(v.newSinceUnpin) + " new " + GlyphArrowEndDown
		indicatorStyled := v.styles.NewIndicator.Render(indicator)
		spaces := strings.Repeat(" ", max(0, v.width-lipgloss.Width(indicator)))
		out.WriteString(spaces)
		out.WriteString(indicatorStyled)
		out.WriteString("\n")
	}

	return out.String()
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
