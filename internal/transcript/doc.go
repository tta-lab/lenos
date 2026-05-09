// Package transcript renders bash-first agent sessions to .md transcript files.
//
// The format is specified in flicknote 57a09f51 ("Bash-First Markdown Render
// Format"). Key design decisions:
//
//   - format.go provides pure markdown formatters with no external dependencies.
//   - MdRecorder is the concrete Recorder consumed by lenos main (cmd/lenos via
//     internal/agent, Phase 1) to write session events as they happen.
//   - writer.go provides a flock-guarded append writer for cross-process safety
//     with the :md protocol handler in temenos.
//   - NoopRecorder is the default for standalone agent-loop tests (Phase 1).
//
// # Concurrency model
//
// MdWriter takes an exclusive advisory flock on the .md file for the duration
// of each Append call (open → flock → write → fsync → unlock → close). This
// provides cross-process serialization between lenos main and the :md protocol
// handler in temenos (writing :md protocol messages as prose blocks).
//
// On Windows, flock is a no-op (writer_windows.go) and concurrent writes from
// multiple processes are NOT detected. This is a known limitation; lenos's
// supported platforms are Unix.
//
// Reference flicknotes:
//   - 7015e7aa — orientation (parent)
//   - 57a09f51 — render format spec (this package implements)
//   - 30666153 — error / edge case handling (E7-E14)
package transcript

// Relationship to internal/session/
//
// internal/session/ holds the SQLite session + Todo CRUD service consumed by
// both the agent loop and the chat UI. internal/transcript/ is the human-facing
// .md render artifact. The two have non-overlapping responsibilities and both stay.
