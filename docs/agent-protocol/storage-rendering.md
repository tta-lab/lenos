# Storage And Rendering

SQLite remains the source of truth for session history. The runtime stores
what the model emitted after any runtime rewrite, plus command results and
runtime-owned narration metadata.

## Assistant Messages

Assistant text stores bash. If natural-language detection rewrites prose, the
stored assistant message is the generated `narrate <<'EOF'` bash, not the raw
prose.

Assistant messages render in the TUI as bash previews. The first line is shown
with a `$` prefix; multi-line bash is collapsed to one preview line.

## Command Results

Command results use `message.CommandContent`:

```go
type CommandContent struct {
    Command     string
    Output      string
    ExitCode    *int
    Pending     bool
    Observation string
    Narrations  []CommandNarration
}
```

`Observation` is the exact result body replayed to the model on the next
iteration. If it is present, `FormatResults` uses it instead of rebuilding from
stdout/stderr.

## Command Narration

Narration is result metadata:

```go
type CommandNarration struct {
    Body             string
    To               string
    DeliveryExitCode *int
    DeliveryOutput   string
}
```

The body renders to the human as markdown, but it is not duplicated into the
next model observation. The model already sees its own assistant command,
including the heredoc body, through the assistant message.

`FormatResults` may include status text such as "narration rendered to user"
or "narration delivery failed", but it omits the narration body.

## TUI Rendering

- Successful commands with no narration are hidden.
- Failed commands render as command output plus the failure badge.
- Commands with narration are rendered even when exit code is 0.
- If both failure and narration exist, the failed result appears first and the
  narration body appears after it as markdown.
