// Package transcript types — Severity, Meta, TrailerToken.
//
// Per spec 57a09f51:
//   - Severity controls runtime-event blockquote prefix (SevNormal → """,
//     SevWarn → "⚠️ ", SevError → "❌ ").
//   - Meta carries YAML frontmatter fields for session start.
//   - TrailerToken is opaque state held across announce→result span.

package transcript

import "time"

// Severity controls the emoji prefix in runtime-event blockquotes.
// Per spec 57a09f51 §Conventions runtime-event severity.
type Severity int

const (
	SevNormal Severity = iota // "> *runtime: <desc>*"
	SevWarn                   // "> *runtime: ⚠️ <desc>*"
	SevError                  // "> *runtime: ❌ <desc>*"
)

// String returns the emoji prefix for the severity level.
// SevNormal returns an empty string; SevWarn returns "⚠️ "; SevError returns "❌ ".
func (s Severity) String() string {
	switch s {
	case SevNormal:
		return ""
	case SevWarn:
		return "⚠️ "
	case SevError:
		return "❌ "
	default:
		return ""
	}
}

// Meta carries the session metadata written as YAML frontmatter.
type Meta struct {
	SessionID string
	Agent     string
	Model     string
	StartedAt time.Time
	Sandbox   string // "on", "off", "degraded", or "" (unset)
	Title     string
	Cwd       string
}

// TrailerToken is an opaque token that callers MUST hold from AgentBashAnnounce
// to either BashResult or BashSkipped. Zero-value TrailerToken{} is valid for
// NoopRecorder so Phase 1 standalone tests don't need real state.
type TrailerToken struct {
	sessionID string
	startedAt time.Time
}
