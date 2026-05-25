# Narrate IPC

`internal/agent/narrate.go` owns the boundary between bash and the Go runtime.

## Shell Prelude

Before executing the model's bash, the runtime prepends a shell function named
`narrate`. It:

1. Parses optional `--to <agent>` and `--continue`.
2. Rejects any remaining positional arguments.
3. Allocates an event directory under `$LENOS_NARRATE_DIR`.
4. Writes the optional addressee to `to`.
5. Writes `continue` when `--continue` was provided.
6. Reads stdin into `body`.
7. Rejects an empty body and removes the event directory.
8. Returns 0 if the event was written.

The function does not end the bash subprocess. Later shell commands still run.
The body must come from stdin, normally a heredoc. `narrate "Done"` fails
because the quoted string is an argument, not stdin.

## Event Format

The IPC format is a temp directory containing one subdirectory per event:

```text
$LENOS_NARRATE_DIR/
  000001.xxxxxx/
    body
    to
    continue
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
a `cat <<'DELIM' | narrate` heredoc pipeline. The delimiter has a fixed prefix
plus random hex. If the body already contains that delimiter on its own line,
generation retries.

## Delivery

Addressed narration is delivered after bash exits:

```bash
cat <<'EOF' | narrate --to agent-name
message
EOF
```

Lenos handles delivery through the `Runner` abstraction after the shell
subprocess exits. Unit tests use a fake runner, so delivery has no side effects
in tests.

Delivery status is stored on `CommandNarration`, not as a separate message.

## Continue Flag

`narrate --continue` still renders the body to the human, but it prevents a
successful narration from ending the loop. The model receives an observation
that says continue was requested while still omitting the narration body.
