package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelp_TogglesOnCtrlG(t *testing.T) {
	a := newTestApp(t, "sess-1", "", &stubWorkspace{})
	a.width = 100
	a.height = 30

	require.False(t, a.helpVisible, "help is hidden by default")
	require.NotContains(t, a.View().Content, "toggle help", "hidden help is not rendered")

	a.Update(keyPress("ctrl+g"))
	assert.True(t, a.helpVisible)
	assert.Contains(t, a.View().Content, "toggle help", "ctrl+g shows the help bindings")

	a.Update(keyPress("ctrl+g"))
	assert.False(t, a.helpVisible, "second ctrl+g hides the overlay")
	assert.NotContains(t, a.View().Content, "toggle help")
}

func TestHelp_RendersAllBindings(t *testing.T) {
	a := newTestApp(t, "sess-1", "", &stubWorkspace{})
	a.width = 120
	a.height = 30
	a.helpVisible = true

	out := a.View().Content

	// Spot-check every keymap entry's help description appears somewhere.
	wants := []string{
		"submit", "toggle header", "toggle queue panel",
		"scroll down", "scroll up", "half page down", "half page up",
		"page down", "page up", "jump to top", "jump to bottom",
		"retry watcher", "toggle help", "quit",
	}
	for _, w := range wants {
		assert.True(t, strings.Contains(out, w), "help overlay missing %q", w)
	}
}
