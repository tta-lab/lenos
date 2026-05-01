// Package tui is the composition root for the interactive lenos TUI.
//
// App owns the chrome — header, viewport, bottom bar, footer, help overlay,
// and input pane — plus the lifecycle coordinators: a fsnotify Watcher over
// the session transcript, TwPoller for taskwarrior subtasks, GitPoller for
// modified files, and NotificationDispatcher for agent-finished desktop
// notifications. App.Update routes pubsub.Event[notify.Notification],
// pubsub.Event[session.Session], and tea.FocusMsg/BlurMsg into the right
// sub-component.
//
// Rendering primitives (dialogs, completions, chat items, attachments,
// styles, image, notification backends, util, common, logo, diffview, list)
// are reused as a library from internal/ui/* — App composes them; the legacy
// composition root has been deleted.
//
// The .md transcript is the source of truth for rendered content. The TUI
// is a window plus an input shell that submits prompts via Workspace.AgentRun.
//
// Reference flicknotes:
//   - 24d493dd — TUI audit + decision log (parent for 680e5b5d)
//   - 7015e7aa — orientation
//   - 57a09f51 — render format spec
//   - 8fbf143f — sage's design (visual reference)
//   - 5a17f0c9 — implementation plan (this package implements)
package tui
