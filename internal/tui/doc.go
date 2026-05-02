// Package tui hosts the transcript-adapter primitives consumed by the
// chat list: block parsing, the Glamour renderer used for body styling,
// and the fsnotify-backed .md watcher.
//
// The package was originally a full Bubble Tea composition root (Steps
// 10–15 of plan 680e5b5d) but the cutover was reverted in commit
// efb94277 — internal/ui/model/ remains the live composition root. Only
// the transcript-domain primitives survived; the chrome modules
// (header / footer / viewport / keys / pills / pollers / notifier)
// were dropped wholesale once the orphan audit confirmed zero external
// production callers.
//
// Public surface:
//
//   - Block / BlockKind / SplitBlocks — transcript block parser
//   - GlyphLambda / AccentAmber / AccentBrass — accent tokens used by chat
//   - BashPromptStyle — the cyan-brass `$` prefix for lenos-bash composites
//   - MarkdownRenderer — Glamour TermRenderer with our theme overrides
//   - Watcher / NewWatcher / Md*Msg — fsnotify wrapper for the .md file
//
// Reference flicknotes:
//   - 7015e7aa — orientation (parent)
//   - 57a09f51 — render format spec
package tui
