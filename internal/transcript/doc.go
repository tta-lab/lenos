// Package transcript renders bash-first agent sessions to .md transcript files.
//
// The format is specified in flicknote 57a09f51 ("Bash-First Markdown Render
// Format"). Key design decisions:
//
//   - Pure formatters (format.go) are stdlib-only so cmd/log (Phase 3) imports
//     them without pulling in database or agent dependencies.
//   - MdRecorder is the concrete Recorder consumed by lenos main (cmd/lenos via
//     internal/agent, Phase 1) to write session events as they happen.
//   - Writer.go provides a flock-guarded append writer for cross-process safety
//     between lenos main and cmd/log.
//   - NoopRecorder is the default for standalone agent-loop tests (Phase 1).
//
// Reference flicknotes:
//   - 7015e7aa — orientation (parent)
//   - 57a09f51 — render format spec (this package implements)
//   - 30666153 — error / edge case handling (E7-E14)
package transcript

// Relationship to internal/session/
//
// internal/session/ is the OLD logos-based session service (sqlite CRUD via
// session.Service interface, used pre-bashfirst). internal/transcript/ is the
// NEW bash-first .md render artifact. They coexist until Phase 5 deletes the
// old one. The agent loop (Phase 1) writes to BOTH — sqlite directly via
// internal/db, and .md via transcript.Recorder.
