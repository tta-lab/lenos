package tui

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"
)

// testdataDir is the directory containing test fixtures (sibling internal/transcript/testdata).
var testdataDir = filepath.Join("..", "transcript", "testdata")

func init() {
	// Ensure lipgloss runewidth is initialized in tests.
	_ = lipgloss.Width("")
}

// testKeyMsg is a test double for tea.KeyMsg.
type testKeyMsg struct {
	text string
}

func (t testKeyMsg) Key() tea.Key   { return tea.Key{Text: t.text} }
func (t testKeyMsg) String() string { return t.text }

// keyPress returns a tea.KeyMsg for testing, matching keys used in KeyMap bindings.
func keyPress(keyStr string) tea.KeyMsg { return testKeyMsg{text: keyStr} }

func TestNew(t *testing.T) {
	// Parallel tests disabled — fsnotify watcher is not goroutine-safe for parallel execution.

	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)
	require.NotNil(t, ui)
	require.Equal(t, "kestrel", ui.fm.Agent)
	require.Equal(t, "claude-sonnet-4-6", ui.fm.Model)
	require.NotNil(t, ui.viewport)
	require.NotNil(t, ui.header)
	require.NotNil(t, ui.footer)
	require.NotNil(t, ui.watcher)
}

func TestNewFileNotFound(t *testing.T) {
	// Parallel tests disabled — fsnotify watcher is not goroutine-safe for parallel execution.

	_, err := New("nonexistent", "/does/not/exist.md")
	require.Error(t, err)
}

func TestInitialState(t *testing.T) {
	// Parallel tests disabled — fsnotify watcher is not goroutine-safe for parallel execution.

	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	// Viewport should be pinned initially.
	require.True(t, ui.viewport.IsPinned(), "viewport should be pinned at startup")

	// Footer should derive idle state.
	deriv := DeriveFooter(ui.md)
	require.Equal(t, FooterStateIdle, deriv.State)
}

func TestWindowSizeResize(t *testing.T) {
	// Parallel tests disabled — fsnotify watcher is not goroutine-safe for parallel execution.

	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	// Simulate a resize to 120x40.
	m, cmd := ui.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	ui = m.(*UI)
	require.Nil(t, cmd, "WindowSizeMsg should not return a command")
	require.Equal(t, 120, ui.width)
	require.Equal(t, 40, ui.height)

	// Viewport should still be pinned after resize.
	require.True(t, ui.viewport.IsPinned(), "viewport should remain pinned after resize")

	// The rendered content should be non-empty.
	view := ui.View()
	require.Greater(t, lipgloss.Width(view.Content), 0, "rendered view should have non-zero width")
}

func TestWindowSizeWhileScrolled(t *testing.T) {
	// Parallel tests disabled — fsnotify watcher is not goroutine-safe for parallel execution.

	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	// Scroll up to unpin.
	m, _ := ui.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	ui = m.(*UI)
	m, _ = ui.Update(keyPress("g"))
	ui = m.(*UI)

	// Verify unpinned.
	require.False(t, ui.viewport.IsPinned(), "viewport should be unpinned after scrolling up")

	// Resize while unpinned.
	m, _ = ui.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	ui = m.(*UI)

	// Should still be unpinned.
	require.False(t, ui.viewport.IsPinned(), "viewport should remain unpinned after resize while scrolled")

	// Content should re-render without panicking.
	_ = ui.View()
}

func TestScrollUpUnpin(t *testing.T) {
	// Parallel tests disabled — fsnotify watcher is not goroutine-safe for parallel execution.

	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)
	require.True(t, ui.viewport.IsPinned())
	// Initially pinned at bottom: offset = totalLines - height.
	initialOffset := ui.viewport.Offset()
	require.Greater(t, initialOffset, 0, "pinned viewport should start at bottom")

	// Scroll up (Home key).
	m, _ := ui.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	ui = m.(*UI)
	m, _ = ui.Update(keyPress("g"))
	ui = m.(*UI)

	require.False(t, ui.viewport.IsPinned(), "viewport should be unpinned after Home")
	require.Equal(t, 0, ui.viewport.Offset(), "g should jump to offset 0")
}

func TestHalfPageDown(t *testing.T) {
	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	// Set a known viewport height and scroll position.
	m, _ := ui.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	ui = m.(*UI)

	// Jump to top first.
	m, _ = ui.Update(keyPress("g"))
	ui = m.(*UI)
	initialOffset := ui.viewport.Offset()

	// Press d (half page down) — offset should advance by roughly height/2 lines.
	m, _ = ui.Update(keyPress("d"))
	ui = m.(*UI)

	require.Greater(t, ui.viewport.Offset(), initialOffset,
		"d should advance offset beyond initial position")
}

func TestHalfPageUp(t *testing.T) {
	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	// Set viewport height.
	m, _ := ui.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	ui = m.(*UI)

	// Jump to bottom, then scroll up.
	m, _ = ui.Update(keyPress("G"))
	ui = m.(*UI)
	require.True(t, ui.viewport.IsPinned())

	// Scroll back up with g.
	m, _ = ui.Update(keyPress("g"))
	ui = m.(*UI)
	initialOffset := ui.viewport.Offset()
	require.Equal(t, 0, initialOffset)

	// Scroll down a bit.
	m, _ = ui.Update(keyPress("d"))
	ui = m.(*UI)
	afterHalfDown := ui.viewport.Offset()

	// Press u (half page up) — should go back toward zero.
	m, _ = ui.Update(keyPress("u"))
	ui = m.(*UI)

	require.Less(t, ui.viewport.Offset(), afterHalfDown,
		"u should reduce offset from half-page-down position")
}

func TestPageDown(t *testing.T) {
	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	m, _ := ui.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	ui = m.(*UI)

	// Jump to top.
	m, _ = ui.Update(keyPress("g"))
	ui = m.(*UI)
	initialOffset := ui.viewport.Offset()

	// Press f (page down) — should advance by a full page.
	m, _ = ui.Update(keyPress("f"))
	ui = m.(*UI)

	require.Greater(t, ui.viewport.Offset(), initialOffset,
		"f should advance offset beyond initial position")
}

func TestPageUp(t *testing.T) {
	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	m, _ := ui.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	ui = m.(*UI)

	// Jump to bottom.
	m, _ = ui.Update(keyPress("G"))
	ui = m.(*UI)
	beforePageUp := ui.viewport.Offset()

	// Page up — should go back.
	m, _ = ui.Update(keyPress("b"))
	ui = m.(*UI)

	require.Less(t, ui.viewport.Offset(), beforePageUp,
		"b should reduce offset from bottom position")
}

func TestEndJumpsToBottom(t *testing.T) {
	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	m, _ := ui.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	ui = m.(*UI)

	// Jump to top.
	m, _ = ui.Update(keyPress("g"))
	ui = m.(*UI)
	require.Equal(t, 0, ui.viewport.Offset())
	require.False(t, ui.viewport.IsPinned())

	// G should jump to bottom and re-pin.
	m, _ = ui.Update(keyPress("G"))
	ui = m.(*UI)

	totalLines := len(ui.viewport.rendered.Lines)
	require.Equal(t, totalLines-ui.viewport.height, ui.viewport.Offset(),
		"End should set offset to totalLines - height")
	require.True(t, ui.viewport.IsPinned())
	require.Equal(t, 0, ui.viewport.newSinceUnpin)
}

func TestEndRePin(t *testing.T) {
	// Parallel tests disabled — fsnotify watcher is not goroutine-safe for parallel execution.

	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	// Scroll up to unpin.
	m, _ := ui.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	ui = m.(*UI)
	m, _ = ui.Update(keyPress("g"))
	ui = m.(*UI)
	require.False(t, ui.viewport.IsPinned())

	// Press G to jump to bottom.
	m, _ = ui.Update(keyPress("G"))
	ui = m.(*UI)

	require.True(t, ui.viewport.IsPinned(), "viewport should re-pin after End")
	require.Equal(t, 0, ui.viewport.newSinceUnpin)
}

func TestFooterActiveOnBashAppend(t *testing.T) {
	// Parallel tests disabled — fsnotify watcher is not goroutine-safe for parallel execution.

	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	// Simulate receiving a bash block append.
	bashBlock := []byte("\n\n```bash\nls\n```\n\n")
	prevWallclock := ui.lastBashWallclock

	m, cmd := ui.Update(MdAppendedMsg{Bytes: bashBlock})
	ui = m.(*UI)

	// Should get a Listen command to continue watching.
	require.NotNil(t, cmd)

	// Footer should now be in active state.
	deriv := DeriveFooter(ui.md)
	require.Equal(t, FooterStateActive, deriv.State,
		"footer should be active after a bash block append")

	// Wallclock should have been reset since a new bash block appeared.
	require.NotEqual(t, prevWallclock, ui.lastBashWallclock,
		"wallclock should reset when new unfinished bash block appears")
}

func TestRuntimeEventDoesNotResetWallclock(t *testing.T) {
	// Parallel tests disabled — fsnotify watcher is not goroutine-safe for parallel execution.

	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	// Transition to active state.
	bashBlock := []byte("\n\n```bash\ngo build\n```\n")
	m, _ := ui.Update(MdAppendedMsg{Bytes: bashBlock})
	ui = m.(*UI)
	require.Equal(t, FooterStateActive, ui.footer.deriv.State)

	// Now append a runtime-event blockquote while in active state.
	prevWallclock := ui.lastBashWallclock
	runtimeEvent := []byte("\n> *runtime: blocked: sed -i not allowed*\n")
	m, _ = ui.Update(MdAppendedMsg{Bytes: runtimeEvent})
	ui = m.(*UI)

	// Wallclock must NOT reset on a runtime-event append.
	require.Equal(t, prevWallclock, ui.lastBashWallclock,
		"wallclock should NOT reset on runtime-event blockquote append")
}

func TestFooterIdleAfterTrailer(t *testing.T) {
	// Parallel tests disabled — fsnotify watcher is not goroutine-safe for parallel execution.

	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	// Simulate a bash block followed by a trailer.
	bashBlock := []byte("\n\n```bash\nls\n```\n")
	trailer := []byte("\n*[14:30:05, 1.2s]*\n")

	m, _ := ui.Update(MdAppendedMsg{Bytes: bashBlock})
	ui = m.(*UI)
	m, _ = ui.Update(MdAppendedMsg{Bytes: trailer})
	ui = m.(*UI)

	// Footer should now be idle/turn-ended.
	deriv := DeriveFooter(ui.md)
	require.True(t, deriv.State == FooterStateTurnEnded || deriv.State == FooterStateIdle,
		"footer should be idle or turn-ended after trailer")
}

func TestTickWhileActive(t *testing.T) {
	// Parallel tests disabled — fsnotify watcher is not goroutine-safe for parallel execution.

	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	// Transition to active state.
	bashBlock := []byte("\n\n```bash\ngo build\n```\n")
	m, _ := ui.Update(MdAppendedMsg{Bytes: bashBlock})
	ui = m.(*UI)

	// Send a tick.
	now := time.Now()
	m, cmd := ui.Update(TickMsg(now))
	ui = m.(*UI)

	// If still active, we should get another tick command.
	if ui.footer.deriv.State == FooterStateActive {
		require.NotNil(t, cmd, "should return Tick command when footer is active")
	} else {
		require.Nil(t, cmd, "should not return Tick command when footer is not active")
	}
}

func TestMdTruncated(t *testing.T) {
	// Parallel tests disabled — fsnotify watcher is not goroutine-safe for parallel execution.

	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	// Scroll up to unpin.
	m, _ := ui.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	ui = m.(*UI)
	m, _ = ui.Update(keyPress("g"))
	ui = m.(*UI)
	require.False(t, ui.viewport.IsPinned())

	// Simulate a truncate (session reset).
	m, cmd := ui.Update(MdTruncatedMsg{})
	ui = m.(*UI)
	require.NotNil(t, cmd, "should return Listen command after truncation")

	// Viewport should be re-pinned after truncate.
	require.True(t, ui.viewport.IsPinned(), "viewport should re-pin after truncation")
}

func TestQuit(t *testing.T) {
	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	// Simulate the ctrl+c key press — Quit key binding triggers tea.Quit.
	m, cmd := ui.Update(keyPress("ctrl+c"))
	ui = m.(*UI)
	// Both are func() Msg. Use reflect to compare function pointers.
	require.Equal(t, reflect.ValueOf(tea.Quit).Pointer(), reflect.ValueOf(cmd).Pointer(),
		"ctrl+c should return tea.Quit command")
	// Also verify invoking the cmd produces a QuitMsg.
	msg := cmd()
	_, ok := msg.(tea.QuitMsg)
	require.True(t, ok, "cmd() should return tea.QuitMsg")
	_ = ui
}

func TestHelpNoOp(t *testing.T) {
	// Parallel tests disabled — fsnotify watcher is not goroutine-safe for parallel execution.

	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	m, cmd := ui.Update(keyPress("ctrl+g"))
	ui = m.(*UI)

	require.Nil(t, cmd, "help key should be a no-op in v1")
	_ = ui.View() // should not panic
}

var (
	_ fmt.Stringer = testKeyMsg{}
	_ tea.KeyMsg   = testKeyMsg{}
)
