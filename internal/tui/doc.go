// Package tui is a pure-.md-viewer Bubble Tea TUI for bash-first sessions.
//
// It watches the session transcript (.md) via fsnotify, renders it with
// Glamour, and provides three UI concerns:
//
//   - Header: 1-row session status from .md frontmatter + turn count
//   - Viewport: virtual-scroll content area with bottom-pin contract
//   - Footer: 1-row status derived purely from .md tail (no pubsub, no state
//     machine)
//
// The .md is the source of truth. The TUI is a window.
//
// Reference flicknotes:
//   - 7015e7aa — orientation (parent)
//   - 57a09f51 — render format spec
//   - 8fbf143f — sage's design (visual language reference)
//   - 5a17f0c9 — implementation plan (this package implements)
package tui
