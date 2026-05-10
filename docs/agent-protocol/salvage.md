# Salvage

Salvage is the runtime's narrow repair path for prose-prefixed bash. It is not
a general bash detector and does not replace the normal bash path.

## Problem

Small models often emit a communication line followed by a real command:

```text
I'll inspect the repo.
cat README.md && ls
```

If treated as markdown, the command is lost. If treated as bash as-is, the
first line executes as a command and fails. Salvage rewrites only the first
line to a bash comment:

```bash
# I'll inspect the repo.
cat README.md && ls
```

The rewritten bash is what gets stored and executed.

## Preconditions

Salvage only runs when:

1. The first line is natural language and is not a markdown heading that starts
   with two or more `#` characters.
2. The remaining body is non-empty.
3. The remaining body is not `:md`, `:continue`, `:exit`, or a bare `exit`.
4. The remaining body is not a blocked command or tool-call hallucination.
5. The remaining body passes `bash -n`.
6. The first effective line of the body has a strong command signal.

Effective lines ignore blank and `#` lines.

## Strong Command Signals

There are two accepted signals.

Path execution:

- The first word starts with `./`, `../`, `/`, or `~/`.
- The same runner reports it executable with `test -x`.

Resolved command:

- The first word starts with lowercase ASCII.
- The same runner resolves it with `command -v`.
- The line has extra shell evidence, such as a flag, redirect, pipe, command
  operator, assignment prefix, or path-like token.

Low-confidence cases fall back to natural-language coercion.

## Runner-Backed Probes

The runtime may execute through `LocalRunner` or `SandboxRunner`. Host `PATH`
and sandbox `PATH` can differ, and relative executable paths must be checked in
the same working directory and filesystem view as the eventual command.

For that reason, salvage probes run through the same `Runner` as execution:

- `command -v -- <word>` for normal command words.
- `test -x <path>` for path execution words.

The probes are read-only and are not persisted as result messages. They are
used only to decide whether a prose-prefixed emit should be rewritten.

## Examples

These should salvage:

```text
I'll test.
go test ./...
```

```text
I'll run the script.
./scripts/test.sh
```

These should not salvage:

```text
### Done
cat README.md && ls
```

```text
Done.
go ahead
```

```text
Done.
make sure tests pass
```

## Tradeoff

Salvage favors false negatives over false positives. Missing a salvage means
the mixed response is delivered as `:md` and the loop stops. Wrongly salvaging
prose executes text the model meant as communication.

If this layer grows into a broad natural-language classifier, it should be
replaced by an explicit classifier component rather than more shell-shaped
heuristics.
