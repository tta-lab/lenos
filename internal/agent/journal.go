package agent

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

//go:embed templates/journal.md
var journalTemplate []byte

const journalsDirName = ".lenos/journals"

// JournalDir returns the path to the journals directory under the working
// directory. It does not create the directory.
func JournalDir(workingDir string) string {
	return filepath.Join(workingDir, journalsDirName)
}

// CreateJournal creates a per-session journal file from the embedded template.
// Returns the absolute path to the created file.
func CreateJournal(workingDir, sessionID string) (string, error) {
	dir := JournalDir(workingDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create journal dir: %w", err)
	}

	path := filepath.Join(dir, sessionID+".md")
	if _, err := os.Stat(path); err == nil {
		// Journal already exists for this session — leave it.
		return path, nil
	}

	if err := os.WriteFile(path, journalTemplate, 0o644); err != nil {
		return "", fmt.Errorf("write journal template: %w", err)
	}

	slog.Debug("Created session journal", "path", path)
	return path, nil
}

// journalSystemHint builds the system message text that tells the agent
// where the journal file is.
func journalSystemHint(journalPath string) string {
	return fmt.Sprintf(
		"Your session journal is at `%s`. "+
			"Read it, fill the initial sections through Plan before editing files, "+
			"and update it when meaningful state changes.",
		journalPath,
	)
}

// isTaskLike returns true if the prompt looks like a task request rather
// than a chat question. A simple heuristic: questions under ~50 chars are
// likely chat; longer or imperative prompts are likely tasks.
func isTaskLike(prompt string) bool {
	trimmed := strings.TrimSpace(prompt)
	if len(trimmed) < 50 && strings.HasSuffix(trimmed, "?") {
		return false
	}
	return true
}

// taskDetectionHint returns the runtime hint for task detection.
func taskDetectionHint() string {
	return "You have received a task. Fill the journal before proceeding — read and fill the initial sections through Plan before editing any files."
}

// periodicCheckHint returns a self-check reminder.
func periodicCheckHint() string {
	return "Reread your journal sections: Environment, Deliverables, Potential Delivery Risks, Existing Verification, Failed Paths, Verification. Are you still on track?"
}

// compactHandoffHint returns a prompt asking the agent to update the journal
// for handoff.
func compactHandoffHint() string {
	return `The user requested "Compact Session". Update your journal for handoff: fill Progress, Decisions, Failed Paths, Verification, Reflection, Next. If done, mark Deliverables complete.

After this turn, the context window will be compacted. When you resume, run ` + "`lenos messages --tail 3`" + ` to recover recent user messages.`
}

// autoCompactHint returns a prompt asking the agent to update the journal
// for handoff because the context window is filling up.
func autoCompactHint() string {
	return `Your context window is filling up. Update your journal for handoff: fill Progress, Decisions, Failed Paths, Verification, Reflection, Next. If done, mark Deliverables complete.

After this turn, the context window will be compacted to make room. When you resume, run ` + "`lenos messages --tail 3`" + ` to recover recent user messages.`
}

// journalExitSummary builds the exit message showing the journal path.
func journalExitSummary(journalPath string) string {
	return fmt.Sprintf("Journal: %s", journalPath)
}
