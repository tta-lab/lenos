package model

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/taskwarrior"
	"github.com/tta-lab/lenos/internal/ui/common"
	"github.com/tta-lab/lenos/internal/ui/styles"
	"github.com/tta-lab/lenos/internal/ui/util"
	"github.com/tta-lab/lenos/internal/workspace"
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
// showing files from git diff --numstat and untracked files.
func (m *UI) modifiedFilesInfo(width, maxItems int, isSection bool) string {
	if !m.gitWorktree {
		return ""
	}

	t := m.com.Styles

	title := t.Subtle.Render("Modified Files")
	if isSection {
		title = common.Section(t, "Modified Files", width)
	}
	list := t.Subtle.Render("None")

	if len(m.modifiedFiles) > 0 {
		list = gitFileList(t, m.modifiedFiles, width, maxItems)
	}

	return lipgloss.NewStyle().Width(width).Render(title + "\n\n" + list)
}

// gitFileList renders a list of modified files with line counts, truncating
// to maxItems.
func gitFileList(t *styles.Styles, files []workspace.ModifiedFile, width, maxItems int) string {
	if maxItems <= 0 || len(files) == 0 {
		return ""
	}

	countWidth := 12
	pathWidth := width - countWidth - 1
	if pathWidth < 10 {
		pathWidth = width
		countWidth = 0
	}

	var lines []string
	for i := 0; i < len(files) && i < maxItems; i++ {
		f := files[i]
		countStr := formatFileCount(t, f)
		countPart := t.Subtle.Render(lipgloss.JoinHorizontal(
			lipgloss.Right,
			lipgloss.NewStyle().Width(countWidth).Render(countStr),
		))
		pathPart := truncatePath(t, f.Path, pathWidth)
		if countWidth > 0 {
			lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Right, countPart, pathPart))
		} else {
			lines = append(lines, pathPart)
		}
	}
	if len(files) > maxItems {
		remaining := len(files) - maxItems
		lines = append(lines, t.Subtle.Render(fmt.Sprintf("…and %d more", remaining)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// formatFileCount returns a colored string for the Added/Deleted counts.
func formatFileCount(t *styles.Styles, f workspace.ModifiedFile) string {
	if f.IsNew {
		return t.Files.Additions.Render("new")
	}
	if f.IsBinary {
		return t.Subtle.Render("bin")
	}
	added := t.Files.Additions.Render(fmt.Sprintf("+%d", f.Added))
	deleted := t.Files.Deletions.Render(fmt.Sprintf("-%d", f.Deleted))
	return added + " " + deleted
}

// truncatePath truncates a path from the left, keeping the filename and
// enough context to identify the file.
func truncatePath(t *styles.Styles, path string, maxWidth int) string {
	rendered := t.Files.Path.Render(path)
	if lipgloss.Width(rendered) <= maxWidth {
		return rendered
	}
	// Truncate from left, preserve trailing component
	parts := strings.Split(path, "/")
	var truncated []string
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.Join(append([]string{"…"}, truncated...), "/")
		if lipgloss.Width(t.Files.Path.Render(candidate)) <= maxWidth {
			truncated = append([]string{parts[i]}, truncated...)
		} else {
			break
		}
	}
	if len(truncated) == 0 {
		return t.Files.Path.Render("…" + path[len(path)-maxWidth:])
	}
	return t.Files.Path.Render(strings.Join(truncated, "/"))
}

// waitNextTWTick returns a command that waits for the next ticker tick and
// emits a twPollMsg. Guards against a nil ticker (poller not started — non-TW
// workers).
func (m *UI) waitNextTWTick() tea.Cmd {
	if m.twPollTicker == nil || m.twJobID == "" {
		return nil
	}
	ticker := m.twPollTicker
	jobID := m.twJobID
	return func() tea.Msg {
		<-ticker.C
		todos, err := taskwarrior.PollSubtasks(context.Background(), jobID)
		if err != nil {
			slog.Warn("TW poll failed", "err", err, "jobID", jobID)
			return twPollMsg{todos: nil}
		}
		return twPollMsg{todos: todos}
	}
}

// startTWTickPoll starts a 500ms ticker that polls taskwarrior subtasks
// for the worker's job. Called once from Init — the poller runs for the UI's
// lifetime since the cwd-derived job hex is stable per worker process.
func (m *UI) startTWTickPoll(jobID string) tea.Cmd {
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
	files []workspace.ModifiedFile
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

// modelInfo renders the current model information including reasoning
// settings and context usage/cost for the sidebar.
func (m *UI) modelInfo(width int) string {
	model := m.selectedLargeModel()
	reasoningInfo := ""
	providerName := ""

	if model != nil {
		// Get provider name first
		providerConfig, ok := m.com.Config().Providers.Get(model.ModelCfg.Provider)
		if ok {
			providerName = providerConfig.Name

			// Only check reasoning if model can reason
			if model.CatwalkCfg.CanReason {
				if len(model.CatwalkCfg.ReasoningLevels) == 0 {
					if model.ModelCfg.Think {
						reasoningInfo = "Thinking On"
					} else {
						reasoningInfo = "Thinking Off"
					}
				} else {
					reasoningEffort := cmp.Or(model.ModelCfg.ReasoningEffort, model.CatwalkCfg.DefaultReasoningEffort)
					reasoningInfo = fmt.Sprintf("Reasoning %s", common.FormatReasoningEffort(reasoningEffort))
				}
			}
		}
	}

	var modelContext *common.ModelContextInfo
	if model != nil && m.session != nil {
		modelContext = &common.ModelContextInfo{
			ContextUsed:  m.session.CompletionTokens + m.session.PromptTokens,
			Cost:         m.session.Cost,
			ModelContext: model.CatwalkCfg.ContextWindow,
		}
	}
	var modelName string
	if model != nil {
		modelName = model.CatwalkCfg.Name
	}
	return common.ModelInfo(m.com.Styles, modelName, providerName, reasoningInfo, modelContext, width)
}
