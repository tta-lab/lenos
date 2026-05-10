# Narrate IPC

`internal/agent/narrate.go` owns the boundary between bash and the Go runtime.

## Shell Prelude

Before executing the model's bash, the runtime prepends a shell function named
`narrate`. It:

1. Parses optional `--to <agent>`.
2. Allocates an event directory under `$LENOS_NARRATE_DIR`.
3. Writes the optional addressee to `to`.
4. Reads stdin into `body`.
5. Returns 0 if the event was written.

The function does not end the bash subprocess. Later shell commands still run.

## Event Format

The IPC format is a temp directory containing one subdirectory per event:

```text
$LENOS_NARRATE_DIR/
  000001.xxxxxx/
    body
    to
  000002.xxxxxx/
    body
```

The prefix comes from a per-process sequence. The suffix comes from `mktemp`.
The runtime sorts event directories lexicographically before reading them.

## Sandbox Scope

The runtime creates `$LENOS_NARRATE_DIR` under the OS temp directory and adds
the temp directory as a writable sandbox path for the command. This keeps IPC
out of the project tree.

## Heredoc Generation

When natural-language detection rewrites prose into bash, the runtime creates
a `narrate <<'DELIM'` heredoc. The delimiter has a fixed prefix plus random
hex. If the body already contains that delimiter on its own line, generation
retries.

## Delivery

Addressed narration is delivered after bash exits:

```bash
cat <<'EOF' | ttal send --to agent-name
message
EOF
```

This delivery call goes through the `Runner` abstraction. Unit tests use a
fake runner, so `ttal send` has no side effects in tests.

Delivery status is stored on `CommandNarration`, not as a separate message.
