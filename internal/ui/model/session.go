package model

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/taskwarrior"
	"github.com/tta-lab/lenos/internal/ui/common"
	"github.com/tta-lab/lenos/internal/ui/styles"
	"github.com/tta-lab/lenos/internal/ui/util"
)

// twPollMsg is sent by the taskwarrior subtask poller with updated todos.
type twPollMsg struct {
	todos []session.Todo
}

// loadSessionMsg is a message indicating that a session and its files have
// been loaded.
type loadSessionMsg struct {
	session *session.Session
}

// loadSession loads the session. It returns a tea.Cmd that, when executed,
// fetches the session data and returns a loadSessionMsg.
func (m *UI) loadSession(sessionID string) tea.Cmd {
	return func() tea.Msg {
		sess, err := m.com.Workspace.GetSession(context.Background(), sessionID)
		if err != nil {
			return util.ReportError(err)
		}
		return loadSessionMsg{session: &sess}
	}
}

// modifiedFilesInfo renders the modified files section for the sidebar,
// showing files from git status --porcelain.
func (m *UI) modifiedFilesInfo(width, maxItems int, isSection bool) string {
	t := m.com.Styles

	title := t.Subtle.Render("Modified Files")
	if isSection {
		title = common.Section(t, "Modified Files", width)
	}
	list := t.Subtle.Render("None")

	if !m.gitWorktree {
		list = t.Subtle.Render("Not a git repository")
	} else if len(m.modifiedFiles) > 0 {
		list = gitFileList(t, m.modifiedFiles, width, maxItems)
	}

	return lipgloss.NewStyle().Width(width).Render(title + "\n\n" + list)
}

// gitFileList renders a list of modified file paths, truncating to maxItems.
func gitFileList(t *styles.Styles, files []string, width, maxItems int) string {
	if maxItems <= 0 || len(files) == 0 {
		return ""
	}
	var lines []string
	for i := 0; i < len(files) && i < maxItems; i++ {
		lines = append(lines, t.Files.Path.Render(files[i]))
	}
	if len(files) > maxItems {
		remaining := len(files) - maxItems
		lines = append(lines, t.Subtle.Render(fmt.Sprintf("…and %d more", remaining)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// stopTWPoll stops the taskwarrior subtask poller and clears its state.
func (m *UI) stopTWPoll() {
	if m.twPollTicker != nil {
		m.twPollTicker.Stop()
		m.twPollTicker = nil
	}
	m.twJobID = ""
	m.twTodos = nil
}

// waitNextTWTick returns a command that waits for the next ticker tick and
// emits a twPollMsg. Guards against a nil ticker (stopped poller).
func (m *UI) waitNextTWTick() tea.Cmd {
	if m.twPollTicker == nil || m.twJobID == "" {
		return nil
	}
	ticker := m.twPollTicker
	jobID := m.twJobID
	return func() tea.Msg {
		_, ok := <-ticker.C
		if !ok {
			return twPollMsg{todos: nil}
		}
		todos, err := taskwarrior.PollSubtasks(context.Background(), jobID)
		if err != nil {
			slog.Warn("TW poll failed", "err", err, "jobID", jobID)
			return twPollMsg{todos: nil}
		}
		return twPollMsg{todos: todos}
	}
}

// startTWTickPoll starts a 500ms ticker that polls taskwarrior subtasks
// and sends twPollMsg when the results change.
func (m *UI) startTWTickPoll(jobID string) tea.Cmd {
	m.stopTWPoll()
	m.twPollTicker = time.NewTicker(500 * time.Millisecond)
	m.twJobID = jobID

	// Initial synchronous poll to populate the UI immediately.
	todos, err := taskwarrior.PollSubtasks(context.Background(), jobID)
	if err != nil {
		slog.Warn("Failed to poll TW subtasks", "err", err)
	} else {
		m.twTodos = todos
	}

	// Return the first scheduled tick via the shared helper.
	return m.waitNextTWTick()
}

// gitPollMsg is sent by the git modified files poller with updated files.
type gitPollMsg struct {
	files []string
}

// stopGitPoll stops the git modified files poller.
func (m *UI) stopGitPoll() {
	if m.gitPollTicker != nil {
		m.gitPollTicker.Stop()
		m.gitPollTicker = nil
	}
}

// waitNextGitTick returns a command that waits for the next ticker tick and
// emits a gitPollMsg. Guards against a nil ticker (stopped poller).
func (m *UI) waitNextGitTick() tea.Cmd {
	if m.gitPollTicker == nil {
		return nil
	}
	ticker := m.gitPollTicker
	return func() tea.Msg {
		_, ok := <-ticker.C
		if !ok {
			return gitPollMsg{files: nil}
		}
		files, err := m.com.Workspace.ListModifiedFiles(context.Background())
		if err != nil {
			slog.Warn("Git poll failed", "err", err)
			return gitPollMsg{files: nil}
		}
		return gitPollMsg{files: files}
	}
}

// startGitPoll starts a 2-second ticker that polls git status and sends
// gitPollMsg when the results change. Idempotent — safe to call multiple times.
func (m *UI) startGitPoll() tea.Cmd {
	if !m.gitWorktree {
		return nil
	}
	m.stopGitPoll()
	m.gitPollTicker = time.NewTicker(2 * time.Second)

	// Return the first scheduled tick.
	return m.waitNextGitTick()
}
