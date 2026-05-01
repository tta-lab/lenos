# internal/tui — Lenos TUI Composition Root

The interactive `lenos` TUI. Watches the per-session transcript on disk,
composes the chrome (header / footer / bottom bar / input pane), and routes
pubsub events from the workspace into UI state.

## Component Diagram

```
┌─────────────────────────────────────────────┐
│ App (Bubble Tea model — composition root)   │
│  ├─ Header   ctrl+d toggles compact/expanded│
│  │          brand · agent · sandbox · ctx%  │
│  │          · TODO x/N · title · git files  │
│  ├─ Viewport scrollable transcript          │
│  │           sticky λ + turn anchors        │
│  ├─ BottomBar ctrl+t toggles compact/full   │
│  │           queue indicator + items        │
│  ├─ Footer   agent state (active/idle/done) │
│  ├─ Help     ctrl+g toggles                 │
│  └─ InputPane textarea + Enter→AgentRun     │
│                                             │
│  Pollers: TwPoller (TODOs, 500ms,           │
│           gated by TTAL_JOB_ID),            │
│           GitPoller (modified files, 2s,    │
│           gated by IsGitWorktree).          │
│  NotificationDispatcher: agent-finished     │
│           desktop notifications, gated on   │
│           focus + DisableNotifications.     │
└─────────────────────────────────────────────┘
       ▲                       ▲
       │ fsnotify              │ pubsub.Event[T]
       │                       │ (notify, session, message)
┌──────────────┐         ┌─────────────┐
│  .md file    │         │ Workspace   │
└──────────────┘         └─────────────┘
```

The `.md` is the source of truth for rendered transcript content.
Workspace pubsub drives notifications and live ctx% / session title
updates. Reused libraries from `internal/ui/{dialog, completions, chat,
attachments, image, notification, styles, anim, util, common, logo,
diffview, list}` provide rendering primitives the App composes.

## Data Flow

```
agent loop → transcript writer → .md file
                                    │
                              fsnotify event
                                    │
                                Watcher.Listen()
                                    │
                                MdAppended / MdTruncated / MdWatchErr
                                    │
                                App.Update
                                    │
                                Render(md) → Glamour → lines
                                    │
                                Viewport.Render → lipgloss → terminal

Workspace pubsub → ws.Subscribe(program) → tea.Program.Send →
        App.Update → notify dispatcher / header.SetSession / ...
```

## Key Bindings

| Key                     | Action                                |
|-------------------------|---------------------------------------|
| `enter`                 | Submit prompt (calls AgentRun)        |
| `ctrl+d`                | Toggle header (compact ↔ expanded)    |
| `ctrl+t`                | Toggle bottom bar (compact ↔ full)    |
| `ctrl+g`                | Toggle help overlay                   |
| `j` / `↓` / `ctrl+j`    | Scroll down 1 line (unpins)           |
| `k` / `↑` / `ctrl+k`    | Scroll up 1 line (unpins)             |
| `d`                     | Half page down                        |
| `u`                     | Half page up                          |
| `f` / `␣` / `pgdn`      | Page down                             |
| `b` / `pgup`            | Page up                               |
| `g` / `home`            | Jump to top                           |
| `G` / `end`             | Jump to bottom (re-pins)              |
| `r`                     | Retry watcher (only after watch err)  |
| `esc`                   | Cancel                                |
| `ctrl+c`                | Quit                                  |

## Footer State Derivation

Footer state is derived from the last 2 KB of markdown — the bottom bar
sits *above* the footer and shows queue depth (when > 0) independently.

| Last content                              | State        | Render                         |
|-------------------------------------------|--------------|--------------------------------|
| Ends with ```` ```bash ```` (unclosed)    | `ACTIVE`     | amber spinner + elapsed        |
| Ends with `*[HH:MM:SS, Xs]*` trailer      | `TURN_ENDED` | dim "turn N ended"             |
| Ends with user message only               | `IDLE`       | dim "awaiting agent"           |
| File empty or whitespace only             | `IDLE`       | dim "awaiting agent"           |

When a watcher error fires (`MdWatchErrMsg`), the header is replaced by a
crimson 1-row banner: `watch error: <err>; r retry · ctrl+c quit`.

## Testing

```bash
go test ./internal/tui/...                   # all tests
go test ./internal/tui/... -run TestApp      # composition root routing
go test ./internal/tui/... -run TestHeader   # collapsible top bar
go test ./internal/tui/... -run TestBottomBar # collapsible bottom bar
go test ./internal/tui/... -run TestRenderer # markdown render
go test ./internal/tui/... -run TestFooter   # footer derivation
go test ./internal/tui/... -run TestViewport # scroll, pin, sticky
go test ./internal/tui/... -run TestWatcher  # fsnotify debounce
```

## References

- [Orientation: Bash-First Architecture (5a17f0c9)](https://flicknote.app/n/5a17f0c9)
- [Parent Orientation (7015e7aa)](https://flicknote.app/n/7015e7aa)
- [Markdown Render Format Spec (57a09f51)](https://flicknote.app/n/57a09f51)
- [TUI Audit + Decision Log (24d493dd)](https://flicknote.app/n/24d493dd)
- [Sage TUI Design (8fbf143f)](https://flicknote.app/n/8fbf143f)
