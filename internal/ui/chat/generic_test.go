package chat

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/ui/list"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

// fakeKeyMsg is a test double for tea.KeyMsg.
type fakeKeyMsg struct {
	s string
}

func (f fakeKeyMsg) String() string { return f.s }
func (f fakeKeyMsg) Key() tea.Key   { return tea.Key{} }

func TestResultMessageItem_HandleKeyEvent(t *testing.T) {
	sty := styles.DefaultStyles()

	makeItem := func(cmd, output string, exitCode *int, pending bool) *ResultMessageItem {
		return &ResultMessageItem{
			highlightableMessageItem: defaultHighlighter(&sty),
			cachedMessageItem:        &cachedMessageItem{},
			focusableMessageItem:     &focusableMessageItem{},
			message: &message.Message{
				ID:   "test-result-1",
				Role: message.Result,
				Parts: []message.ContentPart{
					message.CommandContent{Command: cmd, Output: output, ExitCode: exitCode, Pending: pending},
				},
			},
			sty: &sty,
		}
	}

	t.Run("y key copies command and output", func(t *testing.T) {
		item := makeItem("echo hello", "hello", intPtr(0), false)
		handled, cmd := item.HandleKeyEvent(fakeKeyMsg{s: "y"})
		assert.True(t, handled, "y key should be handled")
		require.NotNil(t, cmd, "copy cmd should be returned")
	})

	t.Run("c key copies command and output", func(t *testing.T) {
		item := makeItem("echo hello", "hello", intPtr(0), false)
		handled, cmd := item.HandleKeyEvent(fakeKeyMsg{s: "c"})
		assert.True(t, handled, "c key should be handled")
		require.NotNil(t, cmd, "copy cmd should be returned")
	})

	t.Run("unrelated key not handled", func(t *testing.T) {
		item := makeItem("echo hello", "hello", intPtr(0), false)
		handled, cmd := item.HandleKeyEvent(fakeKeyMsg{s: "x"})
		assert.False(t, handled, "x key should not be handled")
		assert.Nil(t, cmd)
	})

	t.Run("non-zero exit code suffix appended", func(t *testing.T) {
		item := makeItem("exit 1", "command failed", intPtr(1), false)
		_, cmd := item.HandleKeyEvent(fakeKeyMsg{s: "y"})
		require.NotNil(t, cmd)
	})

	t.Run("pending command copies command only", func(t *testing.T) {
		item := makeItem("sleep 10", "", nil, true)
		_, cmd := item.HandleKeyEvent(fakeKeyMsg{s: "y"})
		require.NotNil(t, cmd)
	})
}

func TestResultMessageItem_Highlightable(t *testing.T) {
	sty := styles.DefaultStyles()
	item := &ResultMessageItem{
		highlightableMessageItem: defaultHighlighter(&sty),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     &focusableMessageItem{},
		message:                  &message.Message{ID: "test"},
		sty:                      &sty,
	}

	// Assert the interface is implemented.
	var _ list.Highlightable = (*ResultMessageItem)(nil)

	// Round-trip: set highlight, then retrieve it.
	// Note: SetHighlight applies an offset of 2 (MessageLeftPaddingTotal).
	item.SetHighlight(0, 0, 1, 12)
	startLine, startCol, endLine, endCol := item.Highlight()
	assert.Equal(t, 0, startLine)
	assert.Equal(t, 0, startCol) // max(0, 0-2)
	assert.Equal(t, 1, endLine)
	assert.Equal(t, 10, endCol) // max(0, 12-2)
}

func TestResultMessageItem_formatCommandForCopy(t *testing.T) {
	sty := styles.DefaultStyles()

	makeItem := func(id string, cmd, output string, exitCode *int, pending bool) *ResultMessageItem {
		return &ResultMessageItem{
			highlightableMessageItem: defaultHighlighter(&sty),
			cachedMessageItem:        &cachedMessageItem{},
			focusableMessageItem:     &focusableMessageItem{},
			message: &message.Message{
				ID: id,
				Parts: []message.ContentPart{
					message.CommandContent{Command: cmd, Output: output, ExitCode: exitCode, Pending: pending},
				},
			},
			sty: &sty,
		}
	}

	t.Run("success exit omits exit code", func(t *testing.T) {
		item := makeItem("f1", "echo hello", "hello", intPtr(0), false)
		got := item.formatCommandForCopy()
		assert.Contains(t, got, "$ echo hello")
		assert.Contains(t, got, "hello")
		assert.NotContains(t, got, "exit code")
	})

	t.Run("non-zero exit includes suffix", func(t *testing.T) {
		item := makeItem("f2", "false", "failed", intPtr(1), false)
		got := item.formatCommandForCopy()
		assert.Contains(t, got, "(exit code: 1)")
	})

	t.Run("non-zero exit no output includes exit suffix", func(t *testing.T) {
		item := makeItem("f2b", "false", "", intPtr(1), false)
		got := item.formatCommandForCopy()
		assert.Contains(t, got, "$ false")
		assert.Contains(t, got, "\n(exit code: 1)")
	})

	t.Run("pending command is command-only", func(t *testing.T) {
		item := makeItem("f3", "sleep 100", "", nil, true)
		got := item.formatCommandForCopy()
		assert.Equal(t, "$ sleep 100", got)
	})

	t.Run("empty output is command-only", func(t *testing.T) {
		item := makeItem("f4", "echo", "", nil, false)
		got := item.formatCommandForCopy()
		assert.Equal(t, "$ echo", got)
	})
}

func intPtr(v int) *int {
	return &v
}
