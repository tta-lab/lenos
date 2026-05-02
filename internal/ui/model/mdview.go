package model

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	"github.com/tta-lab/lenos/internal/tui"
	"github.com/tta-lab/lenos/internal/ui/chat"
)

// lambdaSGR paints the user-prompt λ in phosphor amber + bold so the eye
// catches the start of every turn. Computed once and reused across blocks
// — lipgloss.NewStyle is cheap, but we hold the rendered SGR string for
// the hot path.
var lambdaSGR = lipgloss.NewStyle().
	Foreground(tui.AccentAmber).
	Bold(true).
	Render(tui.GlyphLambda)

// bashPromptStyle paints the leading `$ ` on a lenos-bash composite block.
// Lifted from tui.NewStyles() once at package init so render is a single
// SGR concat per block.
var bashPromptStyle = tui.NewStyles().BashPrompt

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

	// Every block renders through Glamour as plain markdown — bash fences
	// stay monospace, prose gets paragraph spacing, runtime-event
	// blockquotes keep their `> ...` styling. Differentiation between user
	// and agent comes from the per-kind line prefix below (only the user
	// msg gets the terracotta bar; agent activity renders flush).
	renderer, rerr := tui.MarkdownRenderer(contentWidth)
	items := make([]chat.MessageItem, 0, len(blocks))
	for _, b := range blocks {
		kind, rendered := classifyAndRenderBlock(b, renderer, rerr)
		items = append(items, chat.NewMdBlockItem(m.com.Styles, b.ID(), b.Source, rendered, kind))
	}
	m.chat.SetMessages(items...)
}

// classifyAndRenderBlock maps a tui.Block to a chat.MdBlockKind and its
// pre-rendered display string. Lenos-bash composites get the special
// `$ <cmd>` rendering — cmd's first line only, colored prompt, with the
// post-fence output rendered as plain markdown via Glamour. Everything
// else (user msg, output, prose, runtime, legacy bash) goes through
// Glamour as-is.
func classifyAndRenderBlock(b tui.Block, renderer *glamour.TermRenderer, rerr error) (chat.MdBlockKind, string) {
	if b.Kind == tui.BlockBashCmd && isLenosBashSource(b.Source) {
		return chat.MdBlockLenosBash, renderLenosBashSource(b.Source, renderer, rerr)
	}

	rendered := b.Source
	if rerr == nil {
		out, err := renderer.Render(b.Source)
		if err != nil {
			slog.Warn("Render block", "kind", b.Kind, "err", err)
		} else {
			rendered = strings.TrimRight(out, "\n")
		}
	}
	if b.Kind == tui.BlockUserMsg {
		rendered = strings.Replace(rendered, tui.GlyphLambda, lambdaSGR, 1)
		return chat.MdBlockUserMsg, rendered
	}
	return chat.MdBlockOther, rendered
}

// isLenosBashSource returns true if the block source begins with the
// `\`\`\`lenos-bash` fence marker. The composite parser ensures any
// such block carries the cmd + its absorbed output as one Source string.
func isLenosBashSource(source string) bool {
	first := strings.TrimSpace(strings.SplitN(source, "\n", 2)[0])
	return strings.HasPrefix(first, "```lenos-bash") || strings.HasPrefix(first, "``` lenos-bash")
}

// renderLenosBashSource produces the styled view for a lenos-bash
// composite: `$ <cmd-first-line>` (cyan prompt, single-line cmd; multi-line
// cmds collapse to first-line plus "…"), followed by the absorbed output
// rendered as plain markdown via Glamour.
func renderLenosBashSource(source string, renderer *glamour.TermRenderer, rerr error) string {
	cmd, output := splitLenosBashSource(source)
	firstLine, _, multiline := strings.Cut(cmd, "\n")
	cmdLine := bashPromptStyle.Render("$ ") + firstLine
	if multiline {
		cmdLine += " " + bashPromptStyle.Render("…")
	}
	if strings.TrimSpace(output) == "" {
		return cmdLine
	}
	if rerr != nil {
		return cmdLine + "\n" + output
	}
	out, err := renderer.Render(output)
	if err != nil {
		slog.Warn("Render lenos-bash output", "err", err)
		return cmdLine + "\n" + output
	}
	return cmdLine + "\n" + strings.TrimRight(out, "\n")
}

// splitLenosBashSource extracts the cmd inside a lenos-bash fence and
// the post-fence output text from a composite block source. Returns
// ("", source) if the source doesn't open with a lenos-bash fence (defensive
// fallback — the caller already filtered via isLenosBashSource).
func splitLenosBashSource(source string) (cmd, output string) {
	s := strings.TrimLeft(source, "\n")
	nl := strings.Index(s, "\n")
	if nl < 0 {
		return "", source
	}
	rest := s[nl+1:]
	closeIdx := strings.Index(rest, "\n```")
	if closeIdx < 0 {
		return strings.TrimRight(rest, "\n"), ""
	}
	cmd = rest[:closeIdx]
	after := rest[closeIdx+len("\n```"):]
	output = strings.TrimLeft(after, "\n")
	return cmd, output
}
