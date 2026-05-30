# Storage And Rendering

SQLite remains the source of truth for session history. The runtime stores
assistant emits, command results, and runtime-owned diagnostics.

## Assistant Messages

Assistant text stores the Lenos Bash emit, after any runtime auto-repair. Bash
blocks remain protocol-shaped in history so future model calls see valid
examples of what they emitted. Markdown prose outside bash blocks is rendered
from the stored assistant text.

## Command Results

Command results use `message.CommandContent`:

```go
type CommandContent struct {
    Command     string
    Output      string
    ExitCode    *int
    Pending     bool
    Observation string
}
```

`Observation` is the exact result body replayed to the model on the next
iteration. If it is present, `FormatResults` uses it instead of rebuilding from
stdout/stderr.

## TUI Rendering

- Successful commands are hidden.
- Failed commands render as command output plus the failure badge.
- Markdown prose outside bash blocks renders as assistant markdown.
- Runtime diagnostics render as result rows.
