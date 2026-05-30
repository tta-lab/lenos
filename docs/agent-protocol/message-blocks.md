# Bash Tags

Tagged bash blocks are the executable form for Lenos Bash. They let an
assistant response mix Markdown prose and shell while preserving one protocol:
Markdown before the tag, bash inside the tag.

This document is parser-facing. The tag constants and render helpers live in
`internal/agent/lenosbash`; prompts and tests should use those helpers instead
of spelling protocol tags directly.

## Contract

Only text inside the first bash block is executable. Text before the bash block
is Markdown prose.

If bash remains after parsing, Lenos runs the first bash block. If the bash
block fails parser validation, the runtime sends a syntax diagnostic and does
not execute it.

If no bash blocks are present, Lenos renders the Markdown prose and ends the
loop.

Protocol tags are recognized only at column 1 on their own physical lines.
Inline or indented tag text is plain text.

## Syntax

Basic form:

```xml
<bash>
ls -la
</bash>
```

Mixed prose and bash:

```xml
Reading the parser before editing.

<bash>
rg "func Parse" internal/agent
</bash>
```

Literal tags inside heredocs are valid. The parser tracks nested tag depth
inside an open bash block so edit payloads can contain examples:

```xml
<bash>
cat <<'EOF' | src edit main.go
===BEFORE===
<bash>
hello
</bash>
===AFTER===
<bash>
world
</bash>
EOF
</bash>
```

After the first closing bash tag, all remaining text is dropped from
persistence, display, and execution. Non-whitespace dropped tail content is
logged as a warning.

An extra closing bash tag before a bash block opens is ignored. If the parser
reaches end of input while a bash block is still open, it returns a diagnostic
instead of executing partial shell.

## Runtime Tags

Command observations are wrapped in result tags. Runtime-owned repairs and
notifications are wrapped in runtime tags. These tags are model-facing protocol
markers owned by `internal/agent/lenosbash`.

## Invalid Forms

Unclosed bash blocks are invalid:

```xml
<bash>
ls
```

Malformed bash inside a valid bash block is invalid:

```xml
<bash>
if true then
</bash>
```

Fenced bash is Markdown prose, not executable bash:

````markdown
```bash
ls
```
````
