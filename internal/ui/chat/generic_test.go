package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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
	t.Run("y key handled and copy cmd returned", func(t *testing.T) {
		item := makeItem("echo hello", "hello", intPtr(0), false)
		handled, cmd := item.HandleKeyEvent(fakeKeyMsg{s: "y"})
		assert.True(t, handled, "y key should be handled")
		require.NotNil(t, cmd, "copy cmd should be returned")
		assert.Equal(t, "$ echo hello\nhello", item.formatCommandForCopy())
	})
	t.Run("c key also handled", func(t *testing.T) {
		item := makeItem("ls -la", "total 4\ndrwxr-xr-x  3 neil staff  96 Apr 11 .", intPtr(0), false)
		handled, cmd := item.HandleKeyEvent(fakeKeyMsg{s: "c"})
		assert.True(t, handled, "c key should be handled")
		require.NotNil(t, cmd)
		assert.Equal(t, "$ ls -la\ntotal 4\ndrwxr-xr-x  3 neil staff  96 Apr 11 .", item.formatCommandForCopy())
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
		assert.Equal(t, "$ exit 1\ncommand failed\n[1]", item.formatCommandForCopy())
	})
	t.Run("non-zero exit no output", func(t *testing.T) {
		item := makeItem("false", "", intPtr(42), false)
		_, cmd := item.HandleKeyEvent(fakeKeyMsg{s: "y"})
		require.NotNil(t, cmd)
		assert.Equal(t, "$ false\n[42]", item.formatCommandForCopy())
	})
	t.Run("pending command copies command only", func(t *testing.T) {
		item := makeItem("sleep 10", "", nil, true)
		_, cmd := item.HandleKeyEvent(fakeKeyMsg{s: "y"})
		require.NotNil(t, cmd)
		assert.Equal(t, "$ sleep 10", item.formatCommandForCopy())
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
func TestResultMessageItem_RenderEmptyCommandContentIsEmpty(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	item := NewResultMessageItem(&sty, &message.Message{
		ID:   "result-empty",
		Role: message.Result,
		Parts: []message.ContentPart{
			message.CommandContent{},
		},
	})
	assert.Empty(t, strings.TrimSpace(ansi.Strip(item.RawRender(80))))
}
func TestResultMessageItem_RenderSuccessfulResultIsEmpty(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	exitCode := 0
	item := NewResultMessageItem(&sty, &message.Message{
		ID:   "result-mixed",
		Role: message.Result,
		Parts: []message.ContentPart{
			message.CommandContent{
				Command:  "cat main.go",
				Output:   "",
				ExitCode: &exitCode,
				Pending:  false,
			},
		},
	})
	rendered := ansi.Strip(item.RawRender(80))
	assert.Empty(t, strings.TrimSpace(rendered))
}
func TestResultMessageItem_RenderMixedNonZeroShowsFailureOutput(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	exitCode := 1
	item := NewResultMessageItem(&sty, &message.Message{
		ID:   "result-mixed-fail",
		Role: message.Result,
		Parts: []message.ContentPart{
			message.CommandContent{
				Command:  "cat missing.go",
				Output:   "No such file",
				ExitCode: &exitCode,
				Pending:  false,
			},
		},
	})
	rendered := ansi.Strip(item.RawRender(80))
	// Non-zero exit: output and exit badge should appear.
	assert.Contains(t, rendered, "No such file")
	assert.Contains(t, rendered, "1")
}
func TestResultMessageItem_RenderAddsMessagePrefix(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	for _, tc := range []struct {
		name string
		part message.CommandContent
		want string
	}{
		{
			name: "pending",
			part: message.CommandContent{Command: "go test ./...", Pending: true},
			want: "running...",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			item := NewResultMessageItem(&sty, &message.Message{
				ID:    "result-1",
				Role:  message.Result,
				Parts: []message.ContentPart{tc.part},
			})
			rendered := ansi.Strip(item.Render(80))
			assert.Contains(t, rendered, tc.want)
			assert.NotEqual(t, ansi.Strip(item.RawRender(80)), rendered)
		})
	}
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
	t.Run("success exit includes output", func(t *testing.T) {
		item := makeItem("f1", "echo hello", "hello", intPtr(0), false)
		got := item.formatCommandForCopy()
		assert.Equal(t, "$ echo hello\nhello", got)
	})
	t.Run("non-zero exit includes suffix", func(t *testing.T) {
		item := makeItem("f2", "false", "failed", intPtr(1), false)
		got := item.formatCommandForCopy()
		assert.Contains(t, got, "[1]")
	})
	t.Run("non-zero exit no output includes exit suffix", func(t *testing.T) {
		item := makeItem("f2b", "false", "", intPtr(1), false)
		got := item.formatCommandForCopy()
		assert.Contains(t, got, "$ false")
		assert.Contains(t, got, "\n[1]")
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
	t.Run("multi-line output preserved exactly", func(t *testing.T) {
		item := makeItem("f5", "ls -la /tmp", "drwxr-xr-x  4 neil staff  128 Apr 11 tmp\n-rw-r--r--  1 neil staff   64 Apr 11 log", nil, false)
		got := item.formatCommandForCopy()
		assert.Equal(t, "$ ls -la /tmp\ndrwxr-xr-x  4 neil staff  128 Apr 11 tmp\n-rw-r--r--  1 neil staff   64 Apr 11 log", got)
	})
}
func intPtr(v int) *int {
	return &v
}
