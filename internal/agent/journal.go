package agent

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

//go:embed templates/journal.md
var journalTemplate []byte

const journalsDirName = ".lenos/journals"

// JournalDir returns the path to the journals directory under the working
// directory. It does not create the directory.
func JournalDir(workingDir string) string {
	return filepath.Join(workingDir, journalsDirName)
}

// JournalPath returns the per-session journal path for a working directory.
func JournalPath(workingDir, sessionID string) string {
	return filepath.Join(JournalDir(workingDir), sessionID+".md")
}

// CreateJournal creates a per-session journal file from the embedded template.
// Returns the absolute path to the created file.
func CreateJournal(workingDir, sessionID string) (string, error) {
	dir := JournalDir(workingDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create journal dir: %w", err)
	}

	path := JournalPath(workingDir, sessionID)
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

// journalFillHint returns a runtime hint instructing the agent to fill the
// session journal before changing files.
func journalFillHint() string {
	return "You are a coder with a session journal. Fill the journal through Plan\n" +
		"before editing, creating, or modifying files.\n\n" +
		"The journal helps you achieve the target by making the task state explicit:\n" +
		"goal, constraints, plan, risks, failed paths, verification, and next action.\n" +
		"It is your durable working memory and handoff record when context is lost.\n\n" +
		"Use `ei ask` or `ei fetch` if another agent can help you gather context for\n" +
		"a better journal. Treat the journal as the source of truth for task state."
}

// periodicCheckHint returns a self-check reminder.
func periodicCheckHint() string {
	return "Reread your session journal with `cat $LENOS_JOURNAL`, then check these sections: Environment, Deliverables, Potential Delivery Risks, Existing Verification, Failed Paths, Verification. Are you still on track? Anything worth to write down?"
}

// compactHandoffHint returns a prompt asking the agent to update the journal
// for handoff.
func compactHandoffHint() string {
	return `The user requested "Compact Session". Update your journal for handoff: fill Progress, Decisions, Failed Paths, Verification, Reflection, Next. If done, mark Deliverables complete.

After this turn, the context window will be compacted.`
}

// autoCompactHint returns a prompt asking the agent to update the journal
// for handoff because the context window is filling up.
func autoCompactHint() string {
	return `Your context window is filling up. Update your journal for handoff: fill Progress, Decisions, Failed Paths, Verification, Reflection, Next. If done, mark Deliverables complete.

After this turn, the context window will be compacted to make room.`
}
