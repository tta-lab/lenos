package tui

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestViewportScroll(t *testing.T) {
	styles := NewStyles()
	v := NewViewport(100, 10, styles)

	// Create a rendered with 20 lines.
	r := Rendered{
		Lines: make([]string, 20),
	}
	for i := range r.Lines {
		r.Lines[i] = "line " + strconv.Itoa(i)
	}

	t.Run("SetRendered while pinned scrolls to bottom", func(t *testing.T) {
		v.SetRendered(r)
		assert.True(t, v.IsPinned())
		assert.Equal(t, 0, v.newSinceUnpin)
		assert.Equal(t, 10, v.offset) // offset = 20-10 = 10
	})

	t.Run("ScrollUp unpin", func(t *testing.T) {
		v.ScrollUp()
		assert.False(t, v.IsPinned())
		assert.Equal(t, 9, v.offset)
	})

	t.Run("ScrollDown re-pin at bottom", func(t *testing.T) {
		v.ScrollDown()
		assert.True(t, v.IsPinned())
		assert.Equal(t, 0, v.newSinceUnpin)
	})

	t.Run("Home unpin", func(t *testing.T) {
		v.Home()
		assert.False(t, v.IsPinned())
		assert.Equal(t, 0, v.offset)
	})

	t.Run("End re-pin", func(t *testing.T) {
		v.End()
		assert.True(t, v.IsPinned())
		assert.Equal(t, 0, v.newSinceUnpin)
	})
}

func TestViewportUnpinned(t *testing.T) {
	styles := NewStyles()
	v := NewViewport(100, 10, styles)

	r := Rendered{Lines: make([]string, 20)}
	for i := range r.Lines {
		r.Lines[i] = "line " + strconv.Itoa(i)
	}

	// Simulate user reading mid-file.
	v.SetRendered(r)
	v.offset = 0
	v.pinned = false
	v.newSinceUnpin = 0

	// More content arrives.
	r2 := Rendered{Lines: make([]string, 30)}
	for i := range r2.Lines {
		r2.Lines[i] = "line " + strconv.Itoa(i)
	}
	v.SetRendered(r2)

	assert.False(t, v.IsPinned())
	assert.Equal(t, 10, v.newSinceUnpin) // 30 - 20 = 10 new lines
}

func TestViewportSticky(t *testing.T) {
	styles := NewStyles()
	v := NewViewport(100, 20, styles)

	// Render full session to get anchors.
	r := Rendered{
		Lines: make([]string, 50),
		Anchors: []TurnAnchor{
			{Number: 1, HeaderText: "Find the auth bug", StartLine: 5},
			{Number: 2, HeaderText: "Open a PR", StartLine: 30},
		},
	}
	for i := range r.Lines {
		r.Lines[i] = "line " + strconv.Itoa(i)
	}
	v.SetRendered(r)

	t.Run("no sticky at offset 0", func(t *testing.T) {
		v.offset = 0
		assert.Nil(t, v.stickyTurn())
	})

	t.Run("no sticky at exact anchor line", func(t *testing.T) {
		v.offset = 5
		assert.Nil(t, v.stickyTurn())
	})

	t.Run("sticky appears past anchor line", func(t *testing.T) {
		v.offset = 6
		sticky := v.stickyTurn()
		assert.NotNil(t, sticky)
		assert.Equal(t, 1, sticky.Number)
		assert.Equal(t, "Find the auth bug", sticky.HeaderText)
	})

	t.Run("second anchor sticky when scrolled past it", func(t *testing.T) {
		v.offset = 31
		sticky := v.stickyTurn()
		assert.NotNil(t, sticky)
		assert.Equal(t, 2, sticky.Number)
	})
}

func TestViewportSetSize(t *testing.T) {
	styles := NewStyles()
	v := NewViewport(100, 10, styles)

	r := Rendered{Lines: make([]string, 30)}
	for i := range r.Lines {
		r.Lines[i] = "line " + strconv.Itoa(i)
	}
	v.SetRendered(r)

	t.Run("SetSize while pinned preserves pin", func(t *testing.T) {
		v.SetSize(100, 20)
		assert.True(t, v.IsPinned())
		assert.Equal(t, 20, v.height)
		assert.Equal(t, 10, v.offset) // 30-20 = 10
	})
}
