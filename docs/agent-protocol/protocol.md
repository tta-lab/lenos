# Protocol

Lenos uses a bash-first agent loop. The model no longer emits traditional
tool calls, so the runtime must classify each assistant emit before deciding
whether to execute bash, deliver markdown, ask the model to retry, or end the
loop.

## Design Goal

The protocol keeps model output small and terminal-native:

- Bash is the action format.
- Markdown is the communication format.
- Runtime observations are user-role feedback to the next model step.
- The database stores protocol text the model emitted or the runtime explicitly
  added.

The classifier is not a general natural-language detector and not a general
bash detector. It answers one protocol question:

> What should the runtime do with this assistant emit?

## Bash

Any valid bash emit that is not otherwise classified runs in the configured
`Runner`.

```bash
rg "needle" internal/agent
go test ./...
```

Each bash emit creates a pending result message, runs in the runner, then
updates that result with output, exit code, and the observation sent back to the
model.

## Markdown

Assistant communication starts with `:md` on the first line:

```text
:md
Done. The tests pass.
```

Addressed markdown starts with `:md ->agent-name`:

```text
:md ->reviewer
Please review the protocol change.
```

The full `:md` source is stored in the assistant message. Delivery strips only
the protocol line and lifecycle marker before routing. There is no runtime
result or ack message for `:md`.

If the assistant forgets `:md`, natural-language coercion may add it. A first
line that starts with two or more `#` characters is treated as markdown and
stored as a `:md` block instead of being salvaged into bash.

## Continue

`:md` stops the loop by default. A trailing `:continue` keeps the loop alive:

```text
:md
I found the relevant file.
:continue
```

The stored assistant text keeps `:continue`. The routed markdown body strips
it. The next model prompt sees the stored protocol text.

## Exit

Only a bare `exit` or `exit N` emit exits the loop:

```bash
exit
exit 0
exit 1
```

Multi-line bash ending in `exit`, or `cmd && exit`, is just bash. The `exit`
runs inside the subprocess and does not end the agent loop as a protocol
message.

## Invariants

- Explicit `:md` wins over all natural-language guessing.
- Natural-language coercion only adds `:md`; it does not strip protocol text.
- `:md` never receives a runtime result message.
- Bare `exit` is the only non-markdown protocol exit.
- Bash execution always goes through `Runner`.
