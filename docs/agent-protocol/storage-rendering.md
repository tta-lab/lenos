# Storage And Rendering

The database and TUI are part of the protocol. They must preserve the shape the
next model turn will see.

## Assistant Storage

Natural-language coercion stores:

```text
:md
<original emit>
```

Explicit `:md` stores the full source, including:

- the `:md` prefix line.
- an optional `->agent` addressee.
- a trailing `:continue` lifecycle marker.

Prose-prefixed bash salvage stores the rewritten bash:

```bash
# <original first line>
<rest of bash>
```

Bare `exit` stores an assistant finish and stops the loop.

## Markdown Routing

For delivery, `:md` strips the protocol line and strips trailing lifecycle
markers from the body. Storage does not strip them.

Addressed markdown routes through `ttal send --to <agent>`. Delivery failures
are logged, but no runtime result message is appended for `:md`.

## Result Storage

Bash result rows store:

- command
- combined stdout/stderr output
- exit code
- model observation body
- pending/completed state

Pending rows are created before execution and updated after execution.

## TUI Rendering

Assistant `:md` messages render as markdown, not as bash. The `:md` line
remains visible; rendering does not strip protocol text.

Successful completed command results are skipped in the chat view because the
assistant bash emit already represents the command. These result rows still
remain in storage.

The TUI still renders:

- pending result rows.
- failed result rows.
- completed result rows without an exit code.
- runtime text result rows.

This keeps failures and active work visible without spending vertical space on
successful command separators.
