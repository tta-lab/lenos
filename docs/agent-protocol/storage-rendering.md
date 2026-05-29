# Storage And Rendering

SQLite remains the source of truth for session history. The runtime stores
assistant emits, published message blocks, command results, and runtime-owned
delivery diagnostics.

## Assistant Messages

Assistant text stores either bash previews or published message block bodies.
Bash emits render as shell previews. Published message block bodies render as
normal assistant text.

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
- Message block bodies render as assistant markdown.
- Message delivery failures render as result rows.
