package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/workspace"
)

// SandboxProvider abstracts the minimal workspace interface needed by Header.
// Kept narrow so tests can satisfy it without standing up the full Workspace.
type SandboxProvider interface {
	AgentSandboxState() string
	AgentModel() workspace.AgentModel
	AgentName() string
}

// Header renders the session status line with compact/expanded modes.
type Header struct {
	fm        Frontmatter
	com       SandboxProvider
	todos     []session.Todo
	gitFiles  []workspace.ModifiedFile
	sess      *session.Session
	turnCount int
	width     int
	compact   bool
	styles    Styles
}

// NewHeader creates a new Header.
func NewHeader(com SandboxProvider, fm Frontmatter, styles Styles) *Header {
	return &Header{
		fm:      fm,
		com:     com,
		compact: true,
		styles:  styles,
	}
}

// SetWidth sets the terminal width for truncation.
func (h *Header) SetWidth(w int) {
	h.width = w
}

// SetTurnCount sets the turn count.
func (h *Header) SetTurnCount(n int) {
	h.turnCount = n
}

// Toggle flips between compact and expanded view.
func (h *Header) Toggle() {
	h.compact = !h.compact
}

// IsCompact returns whether the header is in compact mode.
func (h *Header) IsCompact() bool {
	return h.compact
}

// SetTodos sets the taskwarrior todos.
func (h *Header) SetTodos(todos []session.Todo) {
	h.todos = todos
}

// SetGitFiles sets the modified git files.
func (h *Header) SetGitFiles(files []workspace.ModifiedFile) {
	h.gitFiles = files
}

// SetSession sets the session for ctx% computation.
func (h *Header) SetSession(s *session.Session) {
	h.sess = s
}

func (h *Header) sandboxState() string {
	if h.com == nil {
		return "degraded"
	}
	return h.com.AgentSandboxState()
}

func (h *Header) sandboxStyle() lipgloss.Style {
	switch h.sandboxState() {
	case "on":
		return h.styles.SandboxOn
	case "off":
		return h.styles.SandboxOff
	default:
		return h.styles.SandboxDegraded
	}
}

func (h *Header) sandboxDesc() string {
	state := h.sandboxState()
	switch state {
	case "on":
		return "sandbox active"
	case "off":
		return "sandbox disabled"
	default:
		return "sandbox degraded (daemon unreachable)"
	}
}

func (h *Header) ctxPct() int {
	if h.com == nil || h.sess == nil {
		return 0
	}
	model := h.com.AgentModel()
	window := model.CatwalkCfg.ContextWindow
	if window <= 0 {
		return 0
	}
	total := h.sess.PromptTokens + h.sess.CompletionTokens
	pct := int((total * 100) / window)
	if pct > 99 {
		return 99
	}
	return pct
}

func (h *Header) todoStatus() (done, total int) {
	for _, t := range h.todos {
		total++
		if t.Status == "completed" {
			done++
		}
	}
	return done, total
}

func (h *Header) formatTodos() string {
	if len(h.todos) == 0 {
		return ""
	}
	var lines []string
	for _, t := range h.todos {
		status := "[ ]"
		if t.Status == "completed" {
			status = "[x]"
		}
		lines = append(lines, fmt.Sprintf("%s %s", status, t.Content))
	}
	return strings.Join(lines, "\n")
}

func (h *Header) renderCompact() string {
	if h.width < 20 {
		return ""
	}

	agent := h.fm.Agent
	if agent == "" {
		agent = "lenos"
	}

	sandboxStyled := h.sandboxStyle().Render("[" + h.sandboxState() + "]")
	ctxPct := h.ctxPct()

	parts := []string{
		h.styles.Brand.Render("Lenos"),
		agent,
		sandboxStyled,
		fmt.Sprintf("%d%%", ctxPct),
	}

	done, total := h.todoStatus()
	if total > 0 {
		parts = append(parts, fmt.Sprintf("TODO %d/%d", done, total))
	}

	parts = append(parts, h.styles.KeystrokeTip.Render("ctrl+d open"))

	text := strings.Join(parts, " · ")

	// Truncate from right if needed.
	if lipgloss.Width(text) > h.width {
		text = truncate(text, h.width)
	}

	return h.styles.Header.Render(text)
}

func (h *Header) renderExpanded() string {
	if h.width < 20 {
		return ""
	}

	var lines []string

	// Row 1: compact row
	lines = append(lines, h.renderCompact())

	// Row 2: title
	title := h.fm.Title
	if title == "" && h.sess != nil {
		title = h.sess.Title
	}
	if title != "" {
		lines = append(lines, "  "+truncate(title, h.width-2))
	}

	// Row 3: git modified files
	if len(h.gitFiles) > 0 {
		lines = append(lines, fmt.Sprintf("  %d modified file(s)", len(h.gitFiles)))
	}

	// Row 4: sandbox detail
	lines = append(lines, "  "+h.sandboxDesc())

	// Row 5: todos
	todos := h.formatTodos()
	if todos != "" {
		for _, line := range strings.Split(todos, "\n") {
			lines = append(lines, "  "+line)
		}
	}

	return strings.Join(lines, "\n")
}

// Render returns the header string.
func (h *Header) Render() string {
	if h.compact {
		return h.renderCompact()
	}
	return h.renderExpanded()
}

// RenderSep returns the header separator.
func (h *Header) RenderSep() string {
	sep := strings.Repeat("─", h.width)
	return h.styles.HeaderSep.Render(sep)
}

func truncate(s string, maxW int) string {
	if lipgloss.Width(s) <= maxW {
		return s
	}
	for lipgloss.Width(s) > maxW-1 && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s[:len(s)-1] + "…"
}
