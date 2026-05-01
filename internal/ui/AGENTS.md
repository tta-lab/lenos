# UI Library Instructions

`internal/ui/` is a **library of reusable TUI primitives** consumed by the
composition root in `internal/tui/`. The old `model/` composition root has
been deleted as of 680e5b5d Step 13; the packages here remain because the
new `tui.App` imports them.

## General Guidelines

- Never use commands to send messages when you can directly mutate children
  or state.
- Keep things simple; do not overcomplicate.
- Create files if needed to separate logic; do not nest models.
- Never do IO or expensive work in `Update`; always use a `tea.Cmd`.
- Never change the model state inside of a command. Use messages and update
  the state in the main `Update` loop.
- Use the `github.com/charmbracelet/x/ansi` package for any string
  manipulation that might involve ANSI codes. Do not manipulate ANSI strings
  at byte level. Some useful functions: `ansi.Cut`, `ansi.StringWidth`,
  `ansi.Strip`, `ansi.Truncate`.

## Architecture

### Library role

`internal/tui` is the only Bubble Tea model in the binary. The packages in
this tree (`dialog/`, `completions/`, `chat/`, `attachments/`, `image/`,
`notification/`, `styles/`, `anim/`, `util/`, `common/`, `logo/`,
`diffview/`, `list/`) expose stateful renderers and helpers that the
composition root composes:

- **Dialog stack** (`dialog/`) — overlay system for command palette,
  session/model/filepicker pickers, OAuth flows, quit confirmation.
- **Completions** (`completions/`) — slash-command + @-mention popup.
- **Chat items** (`chat/messages.go`, `chat/user.go`, `chat/generic.go`,
  `chat/assistant.go`) — `MessageItem` implementations consumed by
  `chat.ExtractMessageItems` (used by `lenos session show`) and by future
  inline rendering.
- **Attachments** (`attachments/`) — chip row above the input editor.
- **Image** (`image/`) — terminal image rendering (Kitty graphics).
- **Notification** (`notification/`) — `Backend` interface +
  `NativeBackend` (beeep) and `NoopBackend` implementations. Wrapped by
  `tui.NotificationDispatcher`.
- **Styles** (`styles/`) — central `Styles` struct passed via
  `*common.Common`.
- **Common** (`common/`) — `Common` struct holds `Workspace` + `Styles`;
  threaded through library components.

### Centralized message handling

Sub-components do not participate in the standard Elm architecture message
loop. They are stateful structs with imperative methods that the main
model calls directly:

- **`list.List`** has no `Update` method at all. The composition root
  calls targeted methods like `HandleMouseDown()`, `ScrollBy()`,
  `SetMessages()`, `Animate()`.
- **`Attachments`** and **`Completions`** have non-standard `Update`
  signatures (e.g., returning `bool` for "consumed") that act as guards,
  not as full Bubble Tea models.

When writing new components, follow this pattern:

- Expose imperative methods for state changes (not `Update(tea.Msg)`).
- Return `tea.Cmd` from methods when side effects are needed.
- Handle rendering via `Render(width int) string` or
  `Draw(scr uv.Screen, area uv.Rectangle)`.

## Key Patterns

### Composition Over Inheritance

Use struct embedding for shared behaviors. See `chat/messages.go` for
examples of reusable embedded structs for highlighting, caching, and focus.

### Interface Hierarchy

The chat message system uses layered interface composition:

- **`list.Item`** — base: `Render(width int) string`
- **`MessageItem`** — extends `list.Item` + `list.RawRenderable` +
  `Identifiable`
- **`ToolMessageItem`** — extends `MessageItem` with tool call/result/status
  methods
- **Opt-in capabilities**: `Focusable`, `Highlightable`, `Expandable`,
  `Animatable`, `Compactable`, `KeyEventHandler`

Key interface locations:

- List item interfaces: `list/item.go`
- Chat message interfaces: `chat/messages.go`
- Tool message interfaces: `chat/tools.go`
- Dialog interface: `dialog/dialog.go`

### Tool Renderers

Each tool has a dedicated renderer in `chat/`. `NewToolMessageItem` in
`chat/tools.go` is the central factory that routes tool names to specific
types:

| File                  | Tools rendered                                 |
| --------------------- | ---------------------------------------------- |
| `chat/bash.go`        | Bash, JobOutput, JobKill                       |
| `chat/write.go`       | Write                                          |
| `chat/generic.go`     | Fallback for unrecognized tools                |
| `chat/user.go`        | User messages (input + attachments)            |

### Styling

- All styles are defined in `styles/styles.go` (large `Styles` struct with
  nested groups for Header, Pills, Dialog, Help, etc.).
- Access styles via `*common.Common` passed to components.
- Use semantic color fields rather than hardcoded colors.

### Dialogs

- Implement the `Dialog` interface in `dialog/dialog.go`:
  `ID()`, `HandleMsg()` returning an `Action`, `Draw()` onto `uv.Screen`.
- `Overlay` manages a stack of dialogs with push/pop/contains operations.
- Dialogs draw last and overlay everything else.
- Use `RenderContext` from `dialog/common.go` for consistent layout (title
  gradients, width, gap, cursor offset helpers).

### Shared Context

The `common.Common` struct holds `Workspace` and `*styles.Styles`. Thread
it through all components that need access to workspace state or styles.

## File Organization

- `chat/` — Chat message item types and tool renderers
- `dialog/` — Dialog implementations (models, sessions, commands,
  permissions, API key, OAuth, filepicker, reasoning, quit)
- `list/` — Generic lazy-rendered scrollable list with viewport tracking
- `common/` — Shared `Common` struct, layout helpers, markdown rendering,
  diff rendering, scrollbar
- `completions/` — Autocomplete popup with filterable list
- `attachments/` — File attachment management
- `styles/` — All style definitions, color tokens, icons
- `diffview/` — Unified and split diff rendering with syntax highlighting
- `anim/` — Animated spinner
- `image/` — Terminal image rendering (Kitty graphics)
- `logo/` — Logo rendering
- `notification/` — Desktop notification backends
- `util/` — Small shared utilities and message types

## Common Gotchas

- Always account for padding/borders in width calculations.
- Use `tea.Batch()` when returning multiple commands.
- Pass `*common.Common` to components that need styles or workspace access.
- When writing `tea.Cmd`s, prefer creating methods on the consumer model
  rather than inline closures so behavior is testable.
- `list.List` only renders visible items (lazy). No render cache exists
  at the list level — items should cache internally if rendering is
  expensive.
