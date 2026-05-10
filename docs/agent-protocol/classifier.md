# Classifier

The classifier turns one assistant emit into a runtime action class.

## Classes

- `exit`: the whole emit is bare `exit` or `exit N`.
- `:md`: explicit markdown protocol, optionally addressed with `:md ->agent`.
- `:md` continue: explicit markdown ending with trailing `:continue`.
- natural language: prose that should be stored as `:md` and stop the loop.
- exec: valid bash to run in the configured runner.
- empty: no useful output, re-prompt the model.
- invalid bash: `bash -n` rejected the emit, re-prompt the model.
- banned: blocked edit forms such as `sed -i` or `perl -i`, re-prompt.
- tool-call hallucination: XML, JSON, or bracket tool-call shapes, re-prompt.

## Order

Classification order matters:

1. Empty emits are rejected first.
2. Bare `exit` stops the loop.
3. Blocked command patterns are rejected before syntax checks.
4. Tool-call-shaped hallucinations are rejected before syntax checks.
5. Explicit `:md` wins before natural-language coercion.
6. A narrow prose-prefixed-bash salvage may rewrite the first line to a bash
   comment.
7. Remaining natural-language emits are stored as `:md` and stop the loop.
8. Remaining emits must pass `bash -n`.
9. Valid remaining emits execute as bash.

This order prevents explicit markdown from being reinterpreted as bash and
prevents hallucinated tool wrappers from being normalized into history.

## Natural-Language Coercion

An emit is treated as natural language when the first non-whitespace byte is
not a lowercase English letter and not `#`.

There is one explicit exception: a first line that starts with two or more `#`
characters is a markdown heading and is treated as natural language. A single
leading `#` remains bash-shaped because it is a shell comment.

If the first byte is uppercase English, the first line must not contain `=`, so
assignment-like shell such as `Output=$(pwd)` can still execute.

When this fires, the runtime prepends `:md\n`, stores that exact protocol text
in the assistant message, and stops the loop. This is an add-only transform:
runtime adds `:md`; it does not strip it later from the database or from the
next model prompt.

Pure CJK prose is covered by this rule because its first byte is neither a
lowercase English letter nor `#`.

## Syntax Check

`bash -n` remains the syntax validator for bash candidates. It only checks
parse validity. It does not execute, inspect `PATH`, or check whether a command
exists.

These are syntactically valid shell commands:

```bash
not_a_real_command
go ahead
make sure tests pass
```

That is why `bash -n` cannot decide whether an ambiguous emit is bash or prose.
