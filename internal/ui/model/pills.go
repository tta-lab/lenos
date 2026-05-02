package model

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

// pillStyle returns the appropriate style for a pill based on focus state.
func pillStyle(focused, panelFocused bool, t *styles.Styles) lipgloss.Style {
	if !panelFocused || focused {
		return t.Pills.Focused
	}
	return t.Pills.Blurred
}

const (
	// pillHeightWithBorder is the height of a pill including its border.
	pillHeightWithBorder = 3
	// maxTaskDisplayLength is the maximum length of a task name in the pill.
	maxTaskDisplayLength = 40
	// maxQueueDisplayLength is the maximum length of a queue item in the list.
	maxQueueDisplayLength = 60
)

// effectiveTodos returns todos to display: TW subtasks when a TW job is active, otherwise nil.
func (m *UI) effectiveTodos() []session.Todo {
	if m.twJobID != "" {
		return m.twTodos
	}
	return nil
}

// hasIncompleteTodos returns true if there are any non-completed todos.
func hasIncompleteTodos(todos []session.Todo) bool {
	return session.HasIncompleteTodos(todos)
}

// hasInProgressTodo returns true if there is at least one in-progress todo.
func hasInProgressTodo(todos []session.Todo) bool {
	for _, todo := range todos {
		if todo.Status == session.TodoStatusInProgress {
			return true
		}
	}
	return false
}

// queuePill renders the queue count pill with gradient triangles.
func queuePill(queue int, focused, panelFocused bool, t *styles.Styles) string {
	if queue <= 0 {
		return ""
	}
	triangles := styles.ForegroundGrad(t, "▶▶▶▶▶▶▶▶▶", false, t.RedDark, t.Secondary)
	if queue < len(triangles) {
		triangles = triangles[:queue]
	}

	text := t.Base.Render(fmt.Sprintf("%d Queued", queue))
	content := fmt.Sprintf("%s %s", strings.Join(triangles, ""), text)
	return pillStyle(focused, panelFocused, t).Render(content)
}

// FormatTodosList formats a list of todos for display.
func FormatTodosList(sty *styles.Styles, todos []session.Todo, inProgressIcon string, width int) string {
	if len(todos) == 0 {
		return ""
	}

	sorted := make([]session.Todo, len(todos))
	copy(sorted, todos)
	sortTodos(sorted)

	var lines []string
	for _, todo := range sorted {
		var prefix string
		textStyle := sty.Base

		switch todo.Status {
		case session.TodoStatusCompleted:
			prefix = sty.Tool.TodoCompletedIcon.Render(styles.TodoCompletedIcon) + " "
		case session.TodoStatusInProgress:
			prefix = sty.Tool.TodoInProgressIcon.Render(inProgressIcon + " ")
		default:
			prefix = sty.Tool.TodoPendingIcon.Render(styles.TodoPendingIcon) + " "
		}

		text := todo.Content
		if todo.Status == session.TodoStatusInProgress && todo.ActiveForm != "" {
			text = todo.ActiveForm
		}
		line := prefix + textStyle.Render(text)
		line = ansi.Truncate(line, width, "…")

		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// sortTodos sorts todos by status: completed, in_progress, pending.
func sortTodos(todos []session.Todo) {
	slices.SortStableFunc(todos, func(a, b session.Todo) int {
		return statusOrder(a.Status) - statusOrder(b.Status)
	})
}

// statusOrder returns the sort order for a todo status.
func statusOrder(s session.TodoStatus) int {
	switch s {
	case session.TodoStatusCompleted:
		return 0
	case session.TodoStatusInProgress:
		return 1
	default:
		return 2
	}
}

// queueList renders the expanded queue items list.
func queueList(queueItems []string, t *styles.Styles) string {
	if len(queueItems) == 0 {
		return ""
	}

	var lines []string
	for _, item := range queueItems {
		text := item
		if len(text) > maxQueueDisplayLength {
			text = text[:maxQueueDisplayLength-1] + "…"
		}
		prefix := t.Pills.QueueItemPrefix.Render() + " "
		lines = append(lines, prefix+t.Muted.Render(text))
	}

	return strings.Join(lines, "\n")
}

// togglePillsExpanded toggles the pills panel expansion state. With TODOs
// living in the header (ctrl+d), the pills area only carries the prompt
// queue — there's nothing to expand when the queue is empty.
func (m *UI) togglePillsExpanded() tea.Cmd {
	if !m.hasSession() {
		return nil
	}
	if m.promptQueue <= 0 {
		return nil
	}
	m.pillsExpanded = !m.pillsExpanded
	m.updateLayoutAndSize()

	// Make sure to follow scroll if follow is enabled when toggling pills.
	if m.chat.Follow() {
		m.chat.ScrollToBottom()
	}

	return nil
}

// pillsAreaHeight calculates the total height needed for the pills area.
// Queue-only since TODOs moved to the header expansion (ctrl+d).
func (m *UI) pillsAreaHeight() int {
	if !m.hasSession() || m.promptQueue <= 0 {
		return 0
	}
	pillsAreaHeight := pillHeightWithBorder
	if m.pillsExpanded {
		pillsAreaHeight += m.promptQueue
	}
	return pillsAreaHeight
}

// renderPills renders the pills panel and stores it in m.pillsView.
// Queue-only — TODOs render in the header expansion (ctrl+d).
func (m *UI) renderPills() {
	m.pillsView = ""
	if !m.hasSession() {
		return
	}

	width := m.layout.pills.Dx()
	if width <= 0 || m.promptQueue <= 0 {
		return
	}

	t := m.com.Styles
	pill := queuePill(m.promptQueue, m.pillsExpanded, m.pillsExpanded, t)
	if pill == "" {
		return
	}

	helpDesc := "open"
	if m.pillsExpanded {
		helpDesc = "close"
	}
	helpKey := t.Pills.HelpKey.Render("ctrl+t")
	helpText := t.Pills.HelpText.Render(helpDesc)
	helpHint := lipgloss.JoinHorizontal(lipgloss.Center, helpKey, " ", helpText)
	pillsRow := lipgloss.JoinHorizontal(lipgloss.Center, pill, " ", helpHint)

	pillsArea := pillsRow
	if m.pillsExpanded && m.com.Workspace.AgentIsReady() {
		queueItems := m.com.Workspace.AgentQueuedPromptsList(m.session.ID)
		if expandedList := queueList(queueItems, t); expandedList != "" {
			pillsArea = lipgloss.JoinVertical(lipgloss.Left, pillsRow, expandedList)
		}
	}

	const paddingLeft = 3
	m.pillsView = t.Pills.Area.MaxWidth(width).PaddingLeft(paddingLeft).Render(pillsArea)
}
