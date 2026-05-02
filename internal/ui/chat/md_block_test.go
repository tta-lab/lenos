package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

// RawRender must yield the verbatim .md source so clipboard / mouse-copy
// paths produce transcript text without ANSI escapes.
func TestMdBlockItem_RawRender_returnsRawSource(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	raw := "**λ** hello\n"
	rendered := "\x1b[33m**λ**\x1b[0m hello\n" // simulated styled output
	item := NewMdBlockItem(&sty, "id-1", raw, rendered, MdBlockUserMsg)

	got := item.RawRender(80)
	assert.Equal(t, raw, got, "RawRender must return rawSource verbatim")
	assert.NotContains(t, got, "\x1b[", "RawRender output must not contain ANSI escapes")
}

// RawRender's highlighted branch must wrap the raw source — never the
// pre-styled rendered string. This locks the contract that the y/c yank
// path produces verbatim text regardless of highlight state.
func TestMdBlockItem_RawRender_highlighted_returnsRawSource_noANSI(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	raw := "go test ./auth\n"
	rendered := "\x1b[36mgo test ./auth\x1b[0m\n"
	item := NewMdBlockItem(&sty, "id-2", raw, rendered, MdBlockOther)
	item.SetHighlight(0, 0, 0, 14)

	got := item.RawRender(80)
	assert.Contains(t, ansi.Strip(got), "go test ./auth", "highlighted RawRender (ANSI-stripped) must contain raw text")
	assert.False(t, strings.Contains(got, "\x1b[36m"), "highlighted RawRender must not embed the pre-styled cyan from rendered")
}

// Focus on a non-user block must produce a visibly different prefix without
// changing the prefix WIDTH — the gutter is always the same number of cells
// (transparent at rest, slate bar when focused) so toggling focus never
// shifts the content. Regression guard for the layout-bug fix.
func TestMdBlockItem_linePrefix_focusedNonUser_changesIdentityNotWidth(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	item := NewMdBlockItem(&sty, "id-fb", "ls -la\n", "ls -la\n", MdBlockOther)

	blurred := item.linePrefix()
	assert.NotEmpty(t, blurred, "non-user blurred block must hold the gutter (otherwise focus toggle shifts content)")

	item.SetFocused(true)
	focused := item.linePrefix()
	assert.NotEmpty(t, focused, "non-user focused block must show a bar prefix")
	assert.NotEqual(t, blurred, focused, "focused prefix must differ from blurred (visual identity change)")

	// Gutter width must be stable across focus toggles — the chars shown may
	// differ (space vs bar), but their displayed width must match so the
	// content column doesn't shift.
	blurredWidth := ansi.StringWidth(blurred)
	focusedWidth := ansi.StringWidth(focused)
	assert.Equal(t, blurredWidth, focusedWidth, "blurred/focused prefix widths must match — no shift on focus toggle")
}

// Render returns the styled rendered string (so the visible chat list keeps
// its Glamour formatting), not the raw source.
func TestMdBlockItem_Render_returnsRendered(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	raw := "go test ./auth\n"
	rendered := "\x1b[36mgo test ./auth\x1b[0m\n"
	item := NewMdBlockItem(&sty, "id-3", raw, rendered, MdBlockOther)

	got := item.Render(80)
	assert.Contains(t, got, "\x1b[36m", "Render output must keep ANSI styling from rendered")
}
