// Package transcript renders bash-first agent sessions to .md transcript files.
//
// The format is specified in flicknote 57a09f51 ("Bash-First Markdown Render
// Format"). Key design decisions:
//
//   - Pure formatters (format.go) are stdlib-only so cmd/narrate (Phase 3)
//     imports them without pulling in database or agent dependencies.
//   - MdRecorder is the concrete Recorder consumed by lenos main (cmd/lenos via
//     internal/agent, Phase 1) to write session events as they happen.
//   - writer.go provides a flock-guarded append writer for cross-process safety
//     between lenos main and cmd/narrate.
//   - NoopRecorder is the default for standalone agent-loop tests (Phase 1).
//
// # Concurrency model
//
// MdWriter takes an exclusive advisory flock on the .md file for the duration
// of each Append call (open → flock → write → fsync → unlock → close). This
// provides cross-process serialization between lenos main (writing structural
// events: bash blocks, trailers, runtime events, output blocks) and Phase 3's
// cmd/narrate binary (writing prose).
//
// Phase 3's cmd/narrate calls AppendStrict directly; the lock semantics live
// in one place. Identical pattern is the contract.
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
// .md render artifact written by lenos main + cmd/narrate. The two have
// non-overlapping responsibilities and both stay.
