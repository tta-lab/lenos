// STDLIB ONLY — this file is imported by cmd/log (Phase 3) and must not pull
// in db/sqlite/agent dependencies.

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
	b.WriteString("---\n\n")
	return b.String()
}

// RenderUserMessage renders a user message line.
// The λ marker prefixes only the first line; multi-line text passes through
// verbatim with no per-line marker.
func RenderUserMessage(text string) string {
	text = strings.TrimRight(text, "\n")
	return fmt.Sprintf("**λ** %s\n\n", text)
}
