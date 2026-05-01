// Package tui contains the bash-first session viewer composition root.
package tui

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"

	"github.com/tta-lab/lenos/internal/agent/notify"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/pubsub"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/ui/common"
)

// App is the top-level Bubble Tea model for the bash-first session viewer.
// It owns the watcher, pollers, dispatcher, sub-components, and pubsub
// routing — the composition root the runtime entry point will wire up.
type App struct {
	com *common.Common

	mdPath    string
	sessionID string
	sess      *session.Session
	md        []byte
	fm        Frontmatter
	rendered  Rendered

	header    *Header
	footer    *Footer
	bottomBar *BottomBar
	viewport  *Viewport
	input     *inputPane

	watcher  *Watcher
	watchErr error

	twPoller  *TwPoller
	gitPoller *GitPoller
	notify    *NotificationDispatcher

	help        help.Model
	helpVisible bool

	width, height     int
	keys              KeyMap
	styles            Styles
	lastBashWallclock time.Time
	triggerMessage    string
}

// New constructs the App. The signature mirrors the legacy model.New so the
// Step 12 cutover in root.go is a one-line swap.
//
// Session resolution order:
//   - continueLast=true → most recent session via ListSessions
//   - sessionID set → GetSession(sessionID)
//   - otherwise → CreateSession("")
//
// Errors during session resolution or initial fs reads are non-fatal: the
// resolved session ID and an empty markdown body fall back, so the watcher
// (and the recoverable error overlay shipped at Step 9) handle the rest.
func New(com *common.Common, sessionID string, continueLast bool, triggerMessage string) *App {
	_ = runewidth.StringWidth("")

	ctx := context.Background()
	sess := resolveSession(ctx, com, sessionID, continueLast)

	dataDir := ""
	if cfg := com.Workspace.Config(); cfg != nil && cfg.Options != nil {
		dataDir, _ = filepath.Abs(cfg.Options.DataDirectory)
	}
	mdPath := filepath.Join(dataDir, "sessions", sess.ID+".md")

	md, _ := os.ReadFile(mdPath) // missing file is fine; watcher will pick up creates
	fm, _, _ := ParseFrontmatter(md)

	styles := NewStyles()

	app := &App{
		com:            com,
		mdPath:         mdPath,
		sessionID:      sess.ID,
		sess:           &sess,
		md:             md,
		fm:             fm,
		styles:         styles,
		keys:           DefaultKeyMap(),
		viewport:       NewViewport(100, 100, styles),
		help:           help.New(),
		triggerMessage: triggerMessage,
	}

	app.header = NewHeader(com.Workspace, fm, styles)
	app.header.SetSession(&sess)
	app.footer = NewFooter(styles)
	app.bottomBar = NewBottomBar(styles, com.Styles)
	app.input = newInputPane()
	app.input.Configure(com, sess.ID)
	app.notify = NewNotificationDispatcher(com.Workspace.Config())

	app.renderContent(100)

	if initial, watcher, err := NewWatcher(mdPath, 5*time.Millisecond); err != nil {
		app.watchErr = err
	} else {
		app.md = initial
		app.watcher = watcher
	}

	app.twPoller, _ = StartTwPoll(os.Getenv("TTAL_JOB_ID"))
	app.gitPoller, _ = StartGitPoll(com.Workspace)

	return app
}

// resolveSession picks the active session per the New() resolution order.
// Returns a zero-value session.Session on unrecoverable failure so callers
// can still construct a UI shell.
func resolveSession(ctx context.Context, com *common.Common, sessionID string, continueLast bool) session.Session {
	if com == nil || com.Workspace == nil {
		return session.Session{}
	}
	switch {
	case continueLast:
		sessions, err := com.Workspace.ListSessions(ctx)
		if err != nil {
			slog.Warn("ListSessions failed for continueLast", "err", err)
			return session.Session{}
		}
		if len(sessions) == 0 {
			return session.Session{}
		}
		return sessions[0]
	case sessionID != "":
		s, err := com.Workspace.GetSession(ctx, sessionID)
		if err != nil {
			slog.Warn("GetSession failed", "id", sessionID, "err", err)
			return session.Session{ID: sessionID}
		}
		return s
	default:
		s, err := com.Workspace.CreateSession(ctx, "")
		if err != nil {
			slog.Error("CreateSession failed", "err", err)
			return session.Session{}
		}
		return s
	}
}

// renderContent re-renders the markdown at the given width and updates all
// sub-components.
func (a *App) renderContent(width int) {
	rendered, err := Render(a.md, width)
	if err != nil {
		rendered = Rendered{
			Lines:        strings.Split(fmt.Sprintf("render error: %v", err), "\n"),
			Anchors:      nil,
			TurnEndCount: 0,
			Frontmatter:  a.fm,
		}
	}
	a.rendered = rendered
	a.viewport.SetSize(width, a.height)
	a.viewport.SetRendered(rendered)
	a.header.SetWidth(width)
	a.header.SetTurnCount(rendered.TurnEndCount)
	a.footer.SetWidth(width)
	a.bottomBar.SetWidth(width)
}

// Init schedules the watcher Listen, footer Tick, initial poller emissions,
// and (when set) the trigger-message dispatch.
func (a *App) Init() tea.Cmd {
	var cmds []tea.Cmd
	if a.watcher != nil {
		cmds = append(cmds, a.watcher.Listen())
	}
	cmds = append(cmds, Tick())

	// Pollers expose their initial cmd via the StartXxxPoll return; here we
	// re-derive by invoking WaitNext, then chain via the *PollMsg handlers.
	if cmd := a.twPoller.WaitNext(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := a.gitPoller.WaitNext(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if a.triggerMessage != "" {
		cmds = append(cmds, a.waitAndTrigger())
	}
	return tea.Batch(cmds...)
}

// waitAndTrigger waits up to five minutes for the agent to be ready, then
// invokes Workspace.AgentRun with the trigger message.
func (a *App) waitAndTrigger() tea.Cmd {
	return func() tea.Msg {
		ws := a.com.Workspace
		deadline := time.Now().Add(5 * time.Minute)
		for !ws.AgentIsReady() {
			if time.Now().After(deadline) {
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
		if err := ws.AgentRun(context.Background(), a.sessionID, a.triggerMessage); err != nil {
			slog.Warn("AgentRun (trigger) failed", "err", err)
		}
		return nil
	}
}

// Update routes messages to sub-components and pollers.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = m.Width
		a.height = m.Height
		a.renderContent(a.width)
		return a, nil

	case MdAppendedMsg:
		prevBashCount := a.rendered.UnfinishedBashCount
		a.md = append(a.md, m.Bytes...)
		a.renderContent(a.width)
		if a.rendered.UnfinishedBashCount > prevBashCount {
			a.lastBashWallclock = time.Now()
		}
		if a.watcher != nil {
			return a, a.watcher.Listen()
		}
		return a, nil

	case MdTruncatedMsg:
		md, err := os.ReadFile(a.mdPath)
		if err != nil {
			slog.Error("re-read session file after truncation", "err", err)
			return a, nil
		}
		a.md = md
		a.renderContent(a.width)
		a.viewport.End()
		if a.watcher != nil {
			return a, a.watcher.Listen()
		}
		return a, nil

	case MdWatchErrMsg:
		a.watchErr = m.Err
		return a, nil

	case TickMsg:
		deriv := DeriveFooter(a.md)
		a.footer.deriv = deriv
		a.footer.lastBashWallclock = a.lastBashWallclock
		a.refreshQueue()
		if deriv.State == FooterStateActive {
			return a, Tick()
		}
		return a, Tick()

	case TwPollMsg:
		a.header.SetTodos(m.Todos)
		return a, a.twPoller.WaitNext()

	case GitPollMsg:
		a.header.SetGitFiles(m.Files)
		return a, a.gitPoller.WaitNext()

	case tea.FocusMsg:
		a.notify.SetFocused(true)
		return a, nil

	case tea.BlurMsg:
		a.notify.SetFocused(false)
		return a, nil

	case pubsub.Event[notify.Notification]:
		a.notify.HandleEvent(m)
		return a, nil

	case pubsub.Event[session.Session]:
		if m.Type == pubsub.UpdatedEvent && m.Payload.ID == a.sessionID {
			payload := m.Payload
			a.sess = &payload
			a.header.SetSession(&payload)
		}
		return a, nil

	case pubsub.Event[message.Message]:
		// Chat content is rendered from the .md transcript (audit M3).
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(m)
	}

	return a, nil
}

// handleKey dispatches keyboard input. Header/bottomBar toggles take
// precedence over viewport scrolls so ctrl+d/ctrl+t never get swallowed.
func (a *App) handleKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	km := a.keys
	switch {
	case key.Matches(m, km.Quit):
		return a, tea.Quit
	case a.watchErr != nil && key.Matches(m, km.Retry):
		initial, watcher, err := NewWatcher(a.mdPath, 5*time.Millisecond)
		if err != nil {
			a.watchErr = err
			return a, nil
		}
		a.md = initial
		a.watcher = watcher
		a.watchErr = nil
		a.renderContent(a.width)
		return a, a.watcher.Listen()
	case key.Matches(m, km.HeaderToggle):
		a.header.Toggle()
		return a, nil
	case key.Matches(m, km.BottomToggle):
		a.bottomBar.Toggle()
		return a, nil
	case key.Matches(m, km.Help):
		a.helpVisible = !a.helpVisible
		return a, nil
	case key.Matches(m, km.Down):
		a.viewport.ScrollDown()
		return a, nil
	case key.Matches(m, km.HalfPageDown):
		a.viewport.HalfPageDown()
		return a, nil
	case key.Matches(m, km.PageDown):
		a.viewport.PageDown()
		return a, nil
	case key.Matches(m, km.End):
		a.viewport.End()
		return a, nil
	case key.Matches(m, km.Up):
		a.viewport.ScrollUp()
		return a, nil
	case key.Matches(m, km.HalfPageUp):
		a.viewport.HalfPageUp()
		return a, nil
	case key.Matches(m, km.PageUp):
		a.viewport.PageUp()
		return a, nil
	case key.Matches(m, km.Home):
		a.viewport.Home()
		return a, nil
	}
	// Anything unclaimed flows into the input pane (textarea + Enter→Submit).
	if cmd := a.input.Update(m); cmd != nil {
		return a, cmd
	}
	return a, nil
}

// refreshQueue reads the current queue state from the workspace and pushes
// it into the BottomBar. Called from the Tick handler — the workspace call
// is in-process and cheap.
func (a *App) refreshQueue() {
	if a.com == nil || a.com.Workspace == nil || a.bottomBar == nil {
		return
	}
	ws := a.com.Workspace
	depth := ws.AgentQueuedPrompts(a.sessionID)
	items := ws.AgentQueuedPromptsList(a.sessionID)
	a.bottomBar.SetQueue(depth, items)
}

// View renders the full TUI layout. When watchErr is non-nil, the header is
// replaced with a recoverable 1-row crimson banner per Step 9.
func (a *App) View() tea.View {
	var topRow string
	if a.watchErr != nil {
		topRow = lipgloss.Place(a.width, 1, lipgloss.Top, lipgloss.Left,
			a.styles.WatchErr.Render(fmt.Sprintf("watch error: %v; r retry · ctrl+c quit", a.watchErr)))
	} else {
		topRow = lipgloss.Place(a.width, 1, lipgloss.Top, lipgloss.Left, a.header.Render())
	}
	sep := lipgloss.Style{}.
		Foreground(lipgloss.Color("245")).
		Render(strings.Repeat("─", maxInt(0, a.width)))

	bottomRows := []string{a.bottomBar.Render(), a.input.Render()}
	chromeBottom := 1 // footer
	for _, r := range bottomRows {
		if r != "" {
			chromeBottom += strings.Count(r, "\n") + 1
		}
	}

	viewportHeight := a.height - 2 - chromeBottom // 1 top + 1 sep + chromeBottom
	if viewportHeight < 1 {
		viewportHeight = 1
	}
	a.viewport.height = viewportHeight
	viewportView := lipgloss.Place(a.width, viewportHeight, lipgloss.Top, lipgloss.Left, a.viewport.Render())

	footerView := lipgloss.Place(a.width, 1, lipgloss.Top, lipgloss.Left, a.footer.Render(time.Now(), a.lastBashWallclock))

	rows := []string{topRow, sep, viewportView}
	if br := a.bottomBar.Render(); br != "" {
		rows = append(rows, br)
	}
	if a.helpVisible {
		a.help.SetWidth(a.width)
		rows = append(rows, a.help.FullHelpView(a.keys.FullHelp()))
	}
	rows = append(rows, footerView)
	if ip := a.input.Render(); ip != "" {
		rows = append(rows, ip)
	}

	return tea.NewView(lipgloss.JoinVertical(lipgloss.Top, rows...))
}

// maxInt returns the larger of two integers.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
