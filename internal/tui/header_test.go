package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHeaderRender(t *testing.T) {
	styles := NewStyles()
	fm := Frontmatter{
		Agent:     "kestrel",
		SessionID: "7d3e8a91-abcd-efgh-ijkl-mnopqrstuvwx",
		Model:     "claude-sonnet-4-6",
	}

	t.Run("full width renders full text", func(t *testing.T) {
		h := NewHeader(fm, styles)
		h.SetWidth(100)
		h.SetTurnCount(2)

		out := h.Render()
		assert.Contains(t, out, "kestrel")
		assert.Contains(t, out, "7d3e8a91")
		assert.Contains(t, out, "claude-sonnet-4-6")
		assert.Contains(t, out, "2 turns")
	})

	t.Run("width 50 renders abbreviated", func(t *testing.T) {
		h := NewHeader(fm, styles)
		h.SetWidth(50)
		h.SetTurnCount(2)

		out := h.Render()
		assert.Contains(t, out, "kestrel")
		assert.Contains(t, out, "7d3e8a91")
		assert.NotContains(t, out, "claude-sonnet-4-6")
	})

	t.Run("turnCount 1 renders singular", func(t *testing.T) {
		h := NewHeader(fm, styles)
		h.SetWidth(100)
		h.SetTurnCount(1)

		out := h.Render()
		assert.Contains(t, out, "1 turn")
		assert.NotContains(t, out, "turns")
	})

	t.Run("long agent name truncates", func(t *testing.T) {
		longFM := Frontmatter{
			Agent:     "this-is-a-very-long-agent-name-that-should-truncate",
			SessionID: "abcd1234",
			Model:     "model-x",
		}
		h := NewHeader(longFM, styles)
		h.SetWidth(30)
		h.SetTurnCount(1)

		out := h.Render()
		assert.Contains(t, out, "…")
	})
}
