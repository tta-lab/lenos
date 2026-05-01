package model

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tta-lab/lenos/internal/tui"
	"github.com/tta-lab/lenos/internal/ui/chat"
)

// lambdaStyle paints the user-prompt λ in phosphor amber + bold so the eye
// catches the start of every turn. Computed once and reused across blocks
// — lipgloss.NewStyle is cheap, but we hold the rendered SGR string for the
// hot path.
var lambdaSGR = lipgloss.NewStyle().
	Foreground(tui.AccentAmber).
	Bold(true).
	Render(tui.GlyphLambda)

// blockquotePrefix prepends `> ` to every line of src so Glamour renders
// it as a blockquote (indented + amber bar marker per MarkdownRenderer's
// BlockQuote style). Used to differentiate agent activity from user msgs
// in the chat list — the user msg renders flush, the bash / output /
// trailer / runtime / prose blocks render quoted under it.
//
// Empty lines stay empty (no trailing space) so Glamour doesn't fragment
// the blockquote across paragraph breaks.
func blockquotePrefix(src string) string {
	if src == "" {
		return src
	}
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + line
		}
	}
	return strings.Join(lines, "\n")
}

// attachMdView (re)attaches the .md watcher for the given session and
// rebuilds the chat list from its current contents. Called from the
// loadSessionMsg handler so the chat panel always reflects the current
// session's transcript.
//
// On error attaching the watcher, mdWatchErr is set; the chat list stays
// usable in a degraded read-only mode (last successfully-built items).
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
	m.rebuildMdBlocks()
	return watcher.Listen()
}

// rebuildMdBlocks parses mdContent into transcript blocks and feeds them
// into m.chat as MessageItems. Each block becomes a navigable / copyable
// / highlightable list item — mirrors the per-message rendering the old
// chat list provided, just sourced from .md instead of pubsub events.
//
// Blocks are pre-rendered through Glamour at the current main-area width
// so the cached output in MdBlockItem doesn't need to re-render on every
// frame; the list re-rebuilds on width changes via updateSize().
func (m *UI) rebuildMdBlocks() {
	width := m.layout.main.Dx()
	if width <= 0 {
		width = 80
	}
	contentWidth := max(width-chat.MessageLeftPaddingTotal, 1)

	blocks := tui.SplitBlocks(m.mdContent)
	if len(blocks) == 0 {
		m.chat.SetMessages()
		return
	}

	// All blocks go through Glamour so bash fences, code, and prose all get
	// proper markdown styling. Non-user blocks are blockquoted (a `> ` line
	// prefix before rendering) so Glamour draws them with its blockquote
	// indent + colored bar — that's the visual differentiation between
	// "what the user said" (flush, with our terracotta bar) and "what the
	// agent did" (Glamour-blockquoted, with amber bar).
	renderer, rerr := tui.MarkdownRenderer(contentWidth)
	items := make([]chat.MessageItem, 0, len(blocks))
	for _, b := range blocks {
		kind := chat.MdBlockOther
		if b.Kind == tui.BlockUserMsg {
			kind = chat.MdBlockUserMsg
		}

		source := b.Source
		if kind == chat.MdBlockOther {
			source = blockquotePrefix(source)
		}

		rendered := source
		if rerr == nil {
			out, err := renderer.Render(source)
			if err != nil {
				slog.Warn("Render block", "kind", b.Kind, "err", err)
			} else {
				rendered = strings.TrimRight(out, "\n")
			}
		}
		if kind == chat.MdBlockUserMsg {
			// Color the leading λ so the start of every turn pops.
			rendered = strings.Replace(rendered, tui.GlyphLambda, lambdaSGR, 1)
		}
		items = append(items, chat.NewMdBlockItem(m.com.Styles, b.ID(), b.Source, rendered, kind))
	}
	m.chat.SetMessages(items...)
}
