# internal/tui — Bash-First Session Viewer

Single-stream `.md` tail viewer for bash-first agent sessions. Watches the session
markdown file, renders it with Glamour, and surfaces live footer state derived from
markdown content.

## Component Diagram

```
internal/tui/
├── doc.go           — package doc
├── tui.go           — Bubble Tea entry point: UI struct, Init/Update/View
├── renderer.go      — Glamour wrapper + turn anchor detection
├── viewport.go      — virtual scroll buffer, bottom-pin, sticky turn marker
├── watcher.go       — fsnotify wrapper, debounced tail reader
├── header.go        — session status bar (agent, model, turn count)
├── footer.go        — live status bar, DeriveFooter from markdown
├── keys.go          — key bindings (j/k/d/u/f/b/g/G/esc/?/ctrl+g/ctrl+c)
├── styles.go        — lipgloss color tokens (amber/slate/crimson)
├── renderer_test.go — anchor detection, Glamour rendering, frontmatter parse
├── viewport_test.go  — scroll, pin/unpin, sticky turn detection
├── watcher_test.go   — debounce, truncation
├── footer_test.go    — state derivation, rendering, duration parsing
├── header_test.go    — header rendering, truncation
└── tui_test.go       — integration: New, resize, scroll, quit, tick
```

## Data Flow

```
.fsnotify event ──► watcher.go ──► MdAppendedMsg ──► tui.go Update
                                                            │
                                        appends to ui.md  ◄─┘
                                                            │
                              ui.renderContent() ──► renderer.go
                                    (Glamour render, anchor detection)
                                                            │
                                          ui.viewport.Render() ──► tea.View
```

Pubsub is **out of scope** in v1. The footer state is derived from markdown
content only (`DeriveFooter` scans the raw bytes for bash fences, trailers, and
runtime events). No pubsub subscription, no agent-side event emission.

## Key Bindings

| Key | Action |
|-----|--------|
| `j` / `↓` / `ctrl+j` | Scroll down 1 line (unpins) |
| `k` / `↑` / `ctrl+k` | Scroll up 1 line (unpins) |
| `d` | Half page down (unpins) |
| `u` | Half page up (unpins) |
| `f` / `space` / `pgdn` | Full page down (unpins) |
| `b` / `pgup` | Full page up (unpins) |
| `g` / `home` | Jump to top (unpins) |
| `G` / `end` | Jump to bottom (re-pins) |
| `?` / `ctrl+g` | Help overlay (no-op in v1) |
| `esc` | Cancel / close overlay (no-op in v1) |
| `ctrl+c` | Quit |

## Footer State Derivation

Footer state is derived from the last 2 KB of markdown:

| Last content | State | Render |
|---|---|---|
| Ends with ```` ```bash ```` (unclosed) | `ACTIVE` | amber spinner + elapsed |
| Ends with `*[HH:MM:SS, Xs]*` trailer | `TURN_ENDED` | dim "turn N ended" |
| Ends with user message only | `IDLE` | dim "awaiting agent" |
| File empty or whitespace only | `IDLE` | dim "awaiting agent" |

No `HALTED` or `RESUMED` states in v1 — those require pubsub agent-side events.

## Testing

```bash
go test ./internal/tui/...              # all tests
go test ./internal/tui/... -run Renderer # renderer + frontmatter only
go test ./internal/tui/... -run Footer   # footer derivation + render
go test ./internal/tui/... -run Viewport # scroll, pin, sticky
go test ./internal/tui/... -run Watcher  # fsnotify debounce + truncation
```

Golden files (`.golden`) are in `testdata/`. Update with:

```bash
go test ./internal/tui/... -update
```

## References

- [Orientation: Bash-First Architecture (5a17f0c9)](https://flicknote.app/n/5a17f0c9)
- [Parent Orientation (7015e7aa)](https://flicknote.app/n/7015e7aa)
- [Markdown Render Format Spec (57a09f51)](https://flicknote.app/n/57a09f51)
- [Sage TUI Design (8fbf143f)](https://flicknote.app/n/8fbf143f) — visual reference only; v1 defers footer state machine, search overlay, pubsub, animated spinner
