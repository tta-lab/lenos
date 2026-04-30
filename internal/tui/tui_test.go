package tui

import (
	"fmt"
	"path/filepath"
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
	code rune
}

func (t testKeyMsg) Key() tea.Key   { return tea.Key{Text: t.text, Code: t.code} }
func (t testKeyMsg) String() string { return t.text }

// keyPress returns a tea.KeyMsg for testing.
func keyPress(keyStr string) tea.KeyMsg {
	code := rune(keyStr[0])
	if len(keyStr) == 1 {
		code = rune(keyStr[0])
	}
	return testKeyMsg{text: keyStr, code: code}
}

func TestNew(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	_, err := New("nonexistent", "/does/not/exist.md")
	require.Error(t, err)
}

func TestInitialState(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)
	require.True(t, ui.viewport.IsPinned())

	// Scroll up (Home key).
	m, _ := ui.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	ui = m.(*UI)
	m, _ = ui.Update(keyPress("g"))
	ui = m.(*UI)

	require.False(t, ui.viewport.IsPinned(), "viewport should be unpinned after Home")
}

func TestEndRePin(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	// Simulate receiving a bash block append.
	bashBlock := []byte("\n\n```bash\nls\n```\n\n")
	m, cmd := ui.Update(MdAppendedMsg{Bytes: bashBlock})
	ui = m.(*UI)

	// Should get a Listen command to continue watching.
	require.NotNil(t, cmd)

	// Footer should now be in active state.
	deriv := DeriveFooter(ui.md)
	require.Equal(t, FooterStateActive, deriv.State,
		"footer should be active after a bash block append")
}

func TestFooterIdleAfterTrailer(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	fixture := filepath.Join(testdataDir, "full_session.md")
	ui, err := New("test-session", fixture)
	require.NoError(t, err)

	// Verify we are not quitting initially.
	require.False(t, ui.quitting, "should not be quitting initially")

	// Send a tea.QuitMsg directly — the key-matching path requires real
	// tea.KeyPressMsg with correct rune values which are hard to construct
	// in tests; send the raw quit message instead.
	m, cmd := ui.Update(tea.QuitMsg{})
	ui = m.(*UI)
	require.True(t, ui.quitting, "QuitMsg should set quitting=true")
	require.Nil(t, cmd, "QuitMsg should not return a command")

	// View should render empty when quitting.
	view := ui.View()
	require.Equal(t, "", view.Content, "View should return empty when quitting")
}

func TestHelpNoOp(t *testing.T) {
	t.Parallel()

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
