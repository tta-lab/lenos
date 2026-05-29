# Storage And Rendering

SQLite remains the source of truth for session history. The runtime stores
assistant emits, published message blocks, command results, and runtime-owned
delivery diagnostics.

## Assistant Messages

Assistant text stores the Lenos Bash emit, after any runtime auto-repair. Bash
emits and `m` message-block emits both remain protocol-shaped in history so
future model calls see valid examples of what they emitted.

Published message block bodies are display data, not assistant emits. Store
them on result rows as narration.

## Command Results

Command results use `message.CommandContent`:

```go
type CommandContent struct {
    Command     string
    Output      string
    ExitCode    *int
    Pending     bool
    Observation string
    Narration   string
}
```

`Observation` is the exact result body replayed to the model on the next
iteration. If it is present, `FormatResults` uses it instead of rebuilding from
stdout/stderr.

`Narration` is the published message-block body for TUI rendering. Narration
does not replay to the model by itself; the assistant emit already contains the
Lenos Bash `m` block.

## TUI Rendering

- Successful commands are hidden.
- Failed commands render as command output plus the failure badge.
- Narration renders as assistant markdown.
- Message delivery failures render as result rows.
