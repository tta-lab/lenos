package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
)

// UI is the top-level Bubble Tea model.
type UI struct {
	width, height int
	mdPath        string
	sessionID     string
	md            []byte // full current content
	fm            Frontmatter
	rendered      Rendered

	header   *Header
	footer   *Footer
	viewport *Viewport

	watcher *Watcher
	keys    KeyMap
	styles  Styles

	lastBashWallclock time.Time
	quitting          bool
}

// New constructs a UI from the session ID and path to a session .md file.
// Reads the file once at construction (so the constructor can fail fast if the
// file is missing). Subsequent updates come via the watcher.
func New(sessionID, mdPath string) (*UI, error) {
	// Initialize runewidth for proper terminal width calculations.
	_ = runewidth.StringWidth("")

	md, err := os.ReadFile(mdPath)
	if err != nil {
		return nil, fmt.Errorf("read session file: %w", err)
	}

	fm, _, err := ParseFrontmatter(md)
	if err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	styles := NewStyles()

	ui := &UI{
		mdPath:    mdPath,
		sessionID: sessionID,
		md:        md,
		fm:        fm,
		styles:    styles,
		keys:      DefaultKeyMap(),
		viewport:  NewViewport(100, 100, styles),
	}

	ui.header = NewHeader(fm, styles)
	ui.footer = NewFooter(styles)

	// Render the initial content.
	ui.renderContent(100)

	// Start watching for changes.
	initial, watcher, err := NewWatcher(mdPath, 5*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("start watcher: %w", err)
	}
	ui.md = initial
	ui.watcher = watcher

	return ui, nil
}

// renderContent re-renders the markdown at the given width and updates all
// sub-components.
func (ui *UI) renderContent(width int) {
	rendered, err := Render(ui.md, width)
	if err != nil {
		// If rendering fails, render an error stub and continue.
		rendered = Rendered{
			Lines:        strings.Split(fmt.Sprintf("render error: %v", err), "\n"),
			Anchors:      nil,
			TurnEndCount: 0,
			Frontmatter:  ui.fm,
		}
	}
	ui.rendered = rendered
	ui.viewport.SetSize(width, ui.height)
	ui.viewport.SetRendered(rendered)
	ui.header.SetWidth(width)
	ui.header.SetTurnCount(rendered.TurnEndCount)
	ui.footer.SetWidth(width)

	// Detect if a new bash block appeared by checking the number of anchors.
	// If anchors grew, a new turn started — update lastBashWallclock.
	if len(rendered.Anchors) > 0 {
		ui.lastBashWallclock = time.Now()
	}
}

// Init starts the file watcher and ticking.
func (ui *UI) Init() tea.Cmd {
	return tea.Batch(
		ui.watcher.Listen(),
		Tick(),
	)
}

// Update handles messages and routes them to sub-components.
func (ui *UI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {

	case tea.WindowSizeMsg:
		ui.width = m.Width
		ui.height = m.Height
		ui.renderContent(ui.width)
		return ui, nil

	case MdAppendedMsg:
		ui.md = append(ui.md, m.Bytes...)
		ui.renderContent(ui.width)
		// If footer state is active, reset the wallclock for duration ticking.
		if ui.footer.deriv.State == FooterStateActive {
			ui.lastBashWallclock = time.Now()
		}
		// Continue watching.
		return ui, ui.watcher.Listen()

	case MdTruncatedMsg:
		// Re-read the entire file from disk; reset state.
		md, err := os.ReadFile(ui.mdPath)
		if err != nil {
			return ui, tea.Quit
		}
		ui.md = md
		ui.renderContent(ui.width)
		ui.viewport.End()
		return ui, ui.watcher.Listen()

	case MdWatchErrMsg:
		fmt.Fprintf(os.Stderr, "watch error: %v\n", m.Err)
		return ui, tea.Quit

	case TickMsg:
		// Re-derive footer state from the latest markdown.
		deriv := DeriveFooter(ui.md)
		ui.footer.deriv = deriv
		ui.footer.lastBashWallclock = ui.lastBashWallclock
		// Keep ticking if the footer is active.
		if deriv.State == FooterStateActive {
			return ui, Tick()
		}
		return ui, nil

	case tea.QuitMsg:
		ui.quitting = true
		return ui, nil

	case tea.KeyMsg:
		km := ui.keys
		switch {
		case key.Matches(m, km.Quit):
			ui.quitting = true
			return ui, nil
		case key.Matches(m, km.Help):
			// Help overlay deferred to v2 — no-op.
			return ui, nil
		case key.Matches(m, km.Down), key.Matches(m, km.HalfPageDown),
			key.Matches(m, km.PageDown), key.Matches(m, km.End):
			ui.viewport.ScrollDown()
			return ui, nil
		case key.Matches(m, km.Up), key.Matches(m, km.HalfPageUp),
			key.Matches(m, km.PageUp), key.Matches(m, km.Home):
			ui.viewport.ScrollUp()
			return ui, nil
		default:
			return ui, nil
		}

	default:
		return ui, nil
	}
}

// View renders the full TUI layout: header, separator, viewport, footer.
func (ui *UI) View() tea.View {
	// On quit, render an empty screen so Bubble Tea can clean up cleanly.
	if ui.quitting {
		return tea.NewView("")
	}

	headerView := lipgloss.Place(ui.width, 1, lipgloss.Top, lipgloss.Left, ui.header.Render())
	sep := lipgloss.Style{}.
		Foreground(lipgloss.Color("245")).
		Render(strings.Repeat("─", maxInt(0, ui.width)))

	// Viewport uses its own height accounting.
	viewportHeight := ui.height - 3 // 1 header + 1 sep + 1 footer
	if viewportHeight < 1 {
		viewportHeight = 1
	}
	ui.viewport.height = viewportHeight
	viewportView := lipgloss.Place(ui.width, viewportHeight, lipgloss.Top, lipgloss.Left, ui.viewport.Render())

	footerView := lipgloss.Place(ui.width, 1, lipgloss.Top, lipgloss.Left, ui.footer.Render(time.Now()))

	return tea.NewView(lipgloss.JoinVertical(lipgloss.Top, headerView, sep, viewportView, footerView))
}

// maxInt returns the larger of two integers.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Run starts the Bubble Tea program in alt-screen mode and blocks until quit.
func Run(sessionID, mdPath string) error {
	ui, err := New(sessionID, mdPath)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(ui).Run()
	return err
}
