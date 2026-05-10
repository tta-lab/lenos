# Runtime Recovery

Malformed emits do not immediately fail the user turn. The runtime appends a
user-role observation and asks the model to try again.

## Empty Emit

Empty output gets a short runtime prompt asking for one of the valid protocol
forms:

- bash
- bash comment
- `:md`
- `exit`

## Invalid Bash

When `bash -n` rejects an emit, the runtime preserves the assistant text and
sends the syntax error back to the model. This branch also suggests `:md`,
because lowercase prose and broken quotes often surface as invalid bash.

Invalid bash is preserved in assistant history because the exact broken shell
text helps the model fix quoting or syntax.

## Blocked Patterns

The runtime blocks in-place edit forms such as `sed -i` and `perl -i` before
execution. The re-prompt asks the model to use `src edit` instead.

This is a runtime nudge, not the main sandbox boundary.

## Tool-Call Hallucinations

XML, JSON, and bracket tool-call shapes are rejected because this protocol has
no tool-calling API.

The runtime deletes the assistant row for this branch. Preserving a fake tool
wrapper in history tends to teach the next turn to imitate it again.

The re-prompt is high-salience and describes valid protocol shapes without
repeating the invalid wrapper syntax verbatim.

## Timeout

If a bash command exceeds the per-call timeout, the result row is updated with
a timeout observation and a non-zero exit code. The model is prompted to use
explicit shell timeout or split the work.

## Command Not Found

If bash reports command not found, the runtime prepends a high-salience
correction before the result envelope.

This covers both cases:

- the model expected a binary that is missing.
- the model emitted prose or markdown fences that bash tried to execute.

The result row is kept with a non-zero exit code.
