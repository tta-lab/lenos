package model

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tta-lab/lenos/internal/tui"
)

// attachMdView (re)attaches the .md watcher and viewport for the given
// session. Called from the loadSessionMsg handler so the chat panel always
// reflects the current session's transcript.
//
// On error attaching the watcher, mdWatchErr is set and the viewport stays
// usable in a degraded read-only mode (last successfully-rendered content).
// Returns the watcher's first Listen cmd so the caller can include it in
// the next tea.Batch.
func (m *UI) attachMdView(sessionID string) tea.Cmd {
	if m.mdWatcher != nil {
		_ = m.mdWatcher.Close()
		m.mdWatcher = nil
	}
	m.mdWatchErr = nil

	cfg := m.com.Workspace.Config()
	if cfg == nil || cfg.Options == nil {
		return nil
	}
	dataDir, err := filepath.Abs(cfg.Options.DataDirectory)
	if err != nil {
		slog.Warn("Resolve data dir for mdView", "err", err)
		return nil
	}
	mdPath := filepath.Join(dataDir, "sessions", sessionID+".md")
	m.mdPath = mdPath

	// Ensure the parent dir + file exist before fsnotify attaches. The
	// transcript writer creates the file lazily on first append; watching a
	// non-existent inode would fire MdWatchErrMsg immediately.
	if err := os.MkdirAll(filepath.Dir(mdPath), 0o755); err != nil {
		slog.Warn("MkdirAll for session .md", "path", mdPath, "err", err)
	}
	if _, err := os.Stat(mdPath); os.IsNotExist(err) {
		if f, err := os.OpenFile(mdPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644); err == nil {
			_ = f.Close()
		}
	}

	initial, watcher, err := tui.NewWatcher(mdPath, 5*time.Millisecond)
	if err != nil {
		m.mdWatchErr = err
		return nil
	}
	m.mdContent = initial
	m.mdWatcher = watcher
	m.renderMdView()
	return watcher.Listen()
}

// renderMdView re-renders the current mdContent through the tui pipeline
// and pushes the result into the chat viewport.
func (m *UI) renderMdView() {
	width := m.layout.main.Dx()
	if width <= 0 {
		width = 80
	}
	r, err := tui.Render(m.mdContent, width)
	if err != nil {
		slog.Warn("Render .md", "path", m.mdPath, "err", err)
		return
	}
	m.mdRendered = r
	if vp := m.chat.MdViewport(); vp != nil {
		vp.SetRendered(r)
	}
}
