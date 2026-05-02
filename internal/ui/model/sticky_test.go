package model

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/ui/chat"
	"github.com/tta-lab/lenos/internal/ui/common"
	"github.com/tta-lab/lenos/internal/ui/list"
)

// stickyTestItem is a minimal MessageItem that opts into the sticky-anchor
// protocol so chat sticky tests don't need to spin up the full md_block
// machinery.
type stickyTestItem struct {
	id     string
	body   string
	anchor bool
}

func (i stickyTestItem) ID() string           { return i.id }
func (i stickyTestItem) Render(int) string    { return i.body }
func (i stickyTestItem) RawRender(int) string { return i.body }
func (i stickyTestItem) IsStickyAnchor() bool { return i.anchor }
func (i stickyTestItem) StickyLine() string {
	if !i.anchor {
		return ""
	}
	return "λ " + i.body
}

var (
	_ chat.MessageItem  = stickyTestItem{}
	_ chat.StickyAnchor = stickyTestItem{}
)

func newStickyTestChat(items ...chat.MessageItem) *Chat {
	c := NewChat(common.DefaultCommon(nil))
	c.SetSize(80, 12) // shrinks list height to 11 (one row reserved for sticky)
	c.SetMessages(items...)
	return c
}

// stickyShows when the chat is scrolled past a user-msg block — the active
// sticky is the latest user-msg at-or-before the top-visible item.
func TestChat_StickyTurn_showsWhenScrolledPastUserMsg(t *testing.T) {
	t.Parallel()

	items := []chat.MessageItem{
		stickyTestItem{id: "u1", body: "first turn body", anchor: true},
		stickyTestItem{id: "b1", body: "tool output 1"},
		stickyTestItem{id: "b2", body: "tool output 2"},
		stickyTestItem{id: "u2", body: "second turn body", anchor: true},
		stickyTestItem{id: "b3", body: "tool output 3"},
	}
	c := newStickyTestChat(items...)
	c.list.ScrollToIndex(2) // tool output between u1 and u2 — u1 is scrolled past

	info, ok := c.StickyTurn()
	require.True(t, ok, "expected sticky to be active")
	assert.Contains(t, ansi.Strip(info.Line), "first turn body")
	assert.Equal(t, 1, info.TurnNumber, "sticky reflects turn 1")
}

// At the very top (offset 0, no head scrolled off), no sticky should render
// even when user-msg blocks exist further down.
func TestChat_StickyTurn_hidesAtTop(t *testing.T) {
	t.Parallel()

	items := []chat.MessageItem{
		stickyTestItem{id: "u1", body: "turn one", anchor: true},
		stickyTestItem{id: "b1", body: "out 1"},
		stickyTestItem{id: "u2", body: "turn two", anchor: true},
	}
	c := newStickyTestChat(items...)
	c.ScrollToTop()

	_, ok := c.StickyTurn()
	assert.False(t, ok, "no sticky when chat is at top")
}

// Walk-back picks the LATEST user-msg block at-or-before the offset. With
// turns at indices 0 and 3, scrolling past idx 4 must show turn 2 (idx 3),
// not turn 1.
func TestChat_StickyTurn_walkBackPicksLatest(t *testing.T) {
	t.Parallel()

	items := []chat.MessageItem{
		stickyTestItem{id: "u1", body: "first", anchor: true},
		stickyTestItem{id: "b1", body: "a"},
		stickyTestItem{id: "b2", body: "b"},
		stickyTestItem{id: "u2", body: "second", anchor: true},
		stickyTestItem{id: "b3", body: "c"},
		stickyTestItem{id: "b4", body: "d"},
	}
	c := newStickyTestChat(items...)
	c.list.ScrollToIndex(5)

	info, ok := c.StickyTurn()
	require.True(t, ok)
	assert.Contains(t, ansi.Strip(info.Line), "second")
	assert.Equal(t, 2, info.TurnNumber)
}

// Zero anchors → no sticky regardless of scroll.
func TestChat_StickyTurn_noAnchorsNoSticky(t *testing.T) {
	t.Parallel()

	items := []chat.MessageItem{
		stickyTestItem{id: "b1", body: "out 1"},
		stickyTestItem{id: "b2", body: "out 2"},
		stickyTestItem{id: "b3", body: "out 3"},
	}
	c := newStickyTestChat(items...)
	c.list.ScrollToIndex(2)

	_, ok := c.StickyTurn()
	assert.False(t, ok)
}

// Quietly assert renderStickyBand returns "" when no sticky active so the
// reserved row stays blank.
func TestChat_renderStickyBand_blankWhenNoSticky(t *testing.T) {
	t.Parallel()

	c := newStickyTestChat()
	out := c.renderStickyBand(40)
	assert.Empty(t, out)
}

// Mouse handlers must subtract the sticky-band row from the chat-area y
// before passing into list coordinates — otherwise every click lands one
// row low (regression: previously the mouse selected the item below where
// the user pointed). This locks the helper at value 1.
func TestChat_chatToListY_subtractsStickyBand(t *testing.T) {
	t.Parallel()
	c := newStickyTestChat()
	assert.Equal(t, 0, c.chatToListY(1), "click on first list row (chat row 1) → list row 0")
	assert.Equal(t, 4, c.chatToListY(5), "click on chat row 5 → list row 4")
	assert.Equal(t, -1, c.chatToListY(0), "click on the sticky band itself → before any item")
}

// Avoid unused import in case list is otherwise unreferenced.
var _ = list.NewList
