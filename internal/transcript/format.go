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

// RenderBashBlock renders a fenced bash block. The bash content is included
// verbatim (no trim), preserving multi-line heredocs exactly.
func RenderBashBlock(bash string) string {
	return fmt.Sprintf("```bash\n%s\n```\n\n", bash)
}

// humanizeDuration formats a duration for display in trailers.
//
// Rules:
//   - <1s    → 3-decimal seconds (e.g. "50ms → 0.050s", "150ms → 0.150s")
//   - 1s..<60s → integer seconds (e.g. "12s")
//   - ≥60s     → <m>m<s>s (e.g. "1m5s")
func humanizeDuration(d time.Duration) string {
	ns := d.Nanoseconds()
	switch {
	case ns < 1_000_000_000:
		// <1s: always 3-decimal seconds (e.g. 50ms → "0.050s",
		// 150ms → "0.150s", 400ms → "0.400s", 999ms → "0.999s").
		return fmt.Sprintf("%.3fs", float64(ns)/1e9)
	case ns < 60_000_000_000:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	default:
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", m, s)
	}
}

// RenderTrailerSuccess renders a success trailer (timing only).
func RenderTrailerSuccess(at time.Time, dur time.Duration) string {
	return fmt.Sprintf("*[%s, %s]*\n\n", at.Format("15:04:05"), humanizeDuration(dur))
}

// RenderTrailerFailure renders a failure trailer with ❌ exit code.
// Signal-derived codes get parenthetical context (SIGINT, killed, SIGTERM).
func RenderTrailerFailure(at time.Time, dur time.Duration, exitCode int) string {
	ctx := signalContext(exitCode)
	if ctx != "" {
		return fmt.Sprintf("*[%s, %s]* — ❌ **exit %d** (%s)\n\n", at.Format("15:04:05"), humanizeDuration(dur), exitCode, ctx)
	}
	return fmt.Sprintf("*[%s, %s]* — ❌ **exit %d**\n\n", at.Format("15:04:05"), humanizeDuration(dur), exitCode)
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

// RenderOutputBlock renders a fenced plain output block with captured stdout/stderr.
// Always includes a trailing blank line. Empty output renders an empty fenced block.
func RenderOutputBlock(out []byte) string {
	if len(out) == 0 {
		return "```\n```\n\n"
	}
	return fmt.Sprintf("```\n%s\n```\n\n", string(out))
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
