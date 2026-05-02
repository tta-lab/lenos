// STDLIB ONLY — this file is imported by cmd/narrate (Phase 3) and must not
// pull in db/sqlite/agent dependencies.

package transcript

import (
	"fmt"
	"strings"
	"time"
)

// RenderFrontmatter renders the YAML frontmatter block for a session.
// Includes a trailing blank line.
func RenderFrontmatter(m Meta) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "session_id: %s\n", m.SessionID)
	fmt.Fprintf(&b, "agent: %s\n", m.Agent)
	fmt.Fprintf(&b, "model: %s\n", m.Model)
	fmt.Fprintf(&b, "started_at: %s\n", m.StartedAt.Format(time.RFC3339))
	if m.Sandbox != "" {
		fmt.Fprintf(&b, "sandbox: %s\n", m.Sandbox)
	}
	if m.Title != "" {
		fmt.Fprintf(&b, "title: %s\n", m.Title)
	}
	if m.Cwd != "" {
		fmt.Fprintf(&b, "cwd: %s\n", m.Cwd)
	}
	b.WriteString("---\n\n")
	return b.String()
}

// RenderUserMessage renders a user message line.
// The λ marker prefixes only the first line; multi-line text passes through
// verbatim with no per-line marker.
func RenderUserMessage(text string) string {
	text = strings.TrimRight(text, "\n")
	return fmt.Sprintf(LambdaMsgPrefix+" %s\n\n", text)
}

// RenderBashBlock renders a fenced `lenos-bash` block — a custom language
// identifier intercepted by the composite block parser in
// internal/tui/blocks.go. The bash content is included verbatim (no trim),
// preserving multi-line heredocs exactly.
func RenderBashBlock(bash string) string {
	return fmt.Sprintf("```lenos-bash\n%s\n```\n\n", bash)
}

// RenderTrailerSuccess renders a success trailer. Successful commands have
// no visible footer in the transcript — the bash block plus its output (if
// any) is the whole story; the prior `*[HH:MM:SS, Xs]*` timestamp footer
// was pure noise in the chat list.
//
// Signature retained for API compatibility; at and dur are unused.
func RenderTrailerSuccess(_ time.Time, _ time.Duration) string {
	return ""
}

// RenderTrailerFailure renders a failure trailer with the ❌ exit code so
// errors stay loud in the transcript. The previous `*[HH:MM:SS, Xs]*`
// timestamp prefix is dropped — the exit code carries the signal value.
// Signal-derived codes get parenthetical context (SIGINT, killed, SIGTERM).
func RenderTrailerFailure(_ time.Time, _ time.Duration, exitCode int) string {
	ctx := signalContext(exitCode)
	if ctx != "" {
		return fmt.Sprintf("❌ **exit %d** (%s)\n\n", exitCode, ctx)
	}
	return fmt.Sprintf("❌ **exit %d**\n\n", exitCode)
}

func signalContext(code int) string {
	switch code {
	case 130:
		return "SIGINT"
	case 137:
		return "killed"
	case 143:
		return "SIGTERM"
	default:
		return ""
	}
}

// RenderOutputBlock renders captured stdout/stderr as plain markdown content.
// Fenced wrapping is intentionally removed — output is rendered as-is so
// Glamour can format it (headings, lists, etc.) and the composite block
// parser in internal/tui/blocks.go groups it with its parent lenos-bash
// fence. Triple-backticks in stdout are sanitized by inserting a zero-width
// space after the first backtick in any ``` sequence, preventing fence
// imbalance in the transcript.
// zwspFenceBreaker inserts a zero-width space (U+200B) between the first
// and second backticks of a literal triple-backtick run so downstream
// markdown parsers don't see a fence. The \u200b escape keeps
// the invisible character visible in source.
const zwspFenceBreaker = "`\u200b``"

func RenderOutputBlock(out []byte) string {
	if len(out) == 0 {
		return ""
	}
	sanitized := strings.ReplaceAll(string(out), "```", zwspFenceBreaker)
	return strings.TrimRight(sanitized, "\n") + "\n\n"
}

// RenderRuntimeEvent renders a severity-prefixed runtime-event blockquote.
// Per spec 57a09f51 §Conventions.
func RenderRuntimeEvent(sev Severity, description string) string {
	return fmt.Sprintf("> *runtime: %s%s*\n\n", sev.String(), description)
}

// RenderTurnEnd renders the *(turn ended)* italic line.
func RenderTurnEnd() string {
	return "*(turn ended)*\n\n"
}

// RenderProse renders plain prose text (used by cmd/narrate for prose content).
// Ensures a single trailing blank line; strips any existing trailing newlines
// from the input first.
func RenderProse(text string) string {
	text = strings.TrimRight(text, "\n")
	return text + "\n\n"
}
