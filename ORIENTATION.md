# TUI Agent Response Rendering Orientation

## Goal

Render Lenos Bash assistant emits as user-facing display parts without changing
the raw assistant message stored in history or sent back to the model.

Example raw assistant message:

```text
m#"
Let me read this file.
"#

cat main.go
```

Target TUI display:

1. Markdown/prose component: `Let me read this file.`
2. Bash command component: `cat main.go`

The raw assistant `TextContent.Text` must remain the original Lenos Bash text.
Provider history should still see the raw message through `message.ToAIMessage`.

## Current State

The runtime already preserves raw assistant emits:

- `internal/agent/loop.go` stores the assistant emit as an assistant
  `TextContent`.
- Mixed message-block-plus-bash emits are parsed with
  `internal/agent/lenosbash.Parse`.
- The cleaned bash is stored on a paired `message.Result` row as
  `CommandContent.Command`.
- The message-block body is stored on that result row as
  `CommandContent.Narration`.
- `message.ToAIMessage` ignores `Narration`, so narration is display-only.

The TUI currently leaks the raw shape:

- `internal/ui/chat/assistant.go` renders ordinary assistant text as a one-line
  bash preview using `bashEmitPreview`.
- That means a raw mixed emit previews as `$ m#"` instead of rendering the
  message-block body.
- `internal/ui/chat/generic.go` renders result narration as markdown, but when
  narration exists it does not also render the command.

So the display is split across the wrong rows: the assistant row shows raw m
syntax, and the result row can show the narration but not the command.

## Recommended Design

Add a UI-only display projection for assistant Lenos Bash emits.

The projection should parse `message.Content().Text` with
`lenosbash.Parse` inside the TUI render layer. It should return ordered display
segments:

- `markdown`: all message block bodies, trimmed and joined with blank lines.
- `bash`: the parsed clean bash, if present.
- `raw`: fallback for invalid or non-Lenos-Bash content.

An agent response should render as at most two main visual areas:

1. A prose/markdown area from all `m` blocks.
2. A bash command area from the cleaned bash.

The mvdan shell fork already supports this split. `lenosbash.Parse` calls
`syntax.ScanMsgBlocks` and returns `parsed.Messages`, `parsed.Bash`, and
`parsed.HasBash`.

Do not add these segments to the DB model unless a real storage need appears.
The current storage contract is useful: raw assistant emit for provider replay,
result narration for display, and command result rows for observations.

Suggested files:

- `internal/ui/chat/assistant.go`: render parsed message blocks as markdown and
  render parsed bash as a single-line command preview.
- `internal/ui/chat/messages.go`: keep extraction simple; assistant messages
  still produce one assistant item.
- `internal/ui/chat/generic.go`: avoid duplicating mixed emit narration when the
  assistant item already renders it. Result rows should still render:
  - narration-only rows,
  - pending command state if desired,
  - non-zero command output and exit badge,
  - legacy/runtime text rows.

## Render Rules

For an assistant message that parses as Lenos Bash:

- Message blocks render as assistant markdown, joined with blank lines.
- Bash renders after the message blocks as one command preview.
- The command preview is one visual line even when the raw bash is multiline.
- Use the parsed clean bash, not the raw source, for the command preview.
- Never show raw `m`, quote delimiters, or message-block targets in normal
  chat display.

For invalid parse or content that is not Lenos Bash:

- Keep the existing fallback: show the one-line bash preview.
- This preserves useful debug behavior for invalid emits and legacy sessions.

For result rows:

- `CommandContent.Narration` with no command remains markdown.
- A successful command result with narration should not render the narration
  again if its assistant message already rendered the message block.
- Pending command result rows can still render `running...`; the assistant row
  already shows the prose and command, while the result row shows execution
  state.
- Successful exit-0 command result rows should disappear after completion, as
  they do today.
- Non-zero command results still render output and the exit badge.
- Provider replay remains unchanged because result `Narration` is display-only.

## Tests To Add

Add focused TUI tests before changing behavior:

- Assistant mixed emit renders markdown body and parsed command.
- Assistant mixed emit does not show raw `m#"` or closing delimiters.
- Multiline bash renders as one command preview line.
- Multiple message blocks render as one markdown area joined with blank lines.
- Message-block-only assistant renders markdown and no command preview.
- Result narration-only rows still render as markdown.
- Mixed successful result rows do not duplicate narration.
- Non-zero mixed result rows still show command failure output.

Good starting files:

- `internal/ui/chat/assistant_test.go`
- `internal/ui/chat/generic_test.go`
- `internal/ui/chat/messages_test.go`

Run:

```sh
go test ./internal/ui/chat
```

If UI message append/update behavior changes, also run:

```sh
go test ./internal/ui/model
```

## Settled Behavior

Pending command rendering stays as-is for the first pass. The assistant item
shows the message and command immediately. The result item shows `running...`
until the command finishes. On exit 0, the successful result row is hidden. On
non-zero exit, the result item shows output plus the exit badge.
