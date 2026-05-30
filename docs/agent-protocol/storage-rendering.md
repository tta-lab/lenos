# Storage And Rendering

SQLite remains the source of truth for session history. The runtime stores
assistant emits, published message blocks, command results, and runtime-owned
delivery diagnostics.

## Assistant Messages

Assistant text stores the Lenos Bash emit, after any runtime auto-repair. Bash
emits and `m` message-block emits both remain protocol-shaped in history so
future model calls see valid examples of what they emitted.

Published message block bodies are display data parsed from assistant emits.
Do not duplicate them onto result rows.

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
- Message-block prose renders from parsed assistant `m` blocks as assistant
  markdown.
- Message delivery failures render as result rows.
