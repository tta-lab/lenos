# Run Tags

Tagged run blocks are the executable form for Lenos Run. They let an
assistant response mix Markdown prose and shell while preserving one protocol:
Markdown before the tag, bash inside the tag.

This document is parser-facing. The tag constants and render helpers live in
`internal/agent/lenosbash`; prompts and tests should use those helpers instead
of spelling protocol tags directly.

## Contract

Only text inside the first run block is executable. Text before the run block
is Markdown prose.

If a run block remains after parsing, Lenos runs the first run block. If the run
block fails parser validation, the runtime sends a syntax diagnostic and does
not execute it.

If no run blocks are present, Lenos renders the Markdown prose and ends the
loop.

Protocol tags are recognized only at column 1 on their own physical lines.
Inline or indented tag text is plain text.

## Syntax

Basic form:

```xml
<run>
ls -la
</run>
```

Mixed prose and bash:

```xml
Reading the parser before editing.

<run>
rg "func Parse" internal/agent
</run>
```

Literal tags inside heredocs are valid. The parser tracks nested tag depth
inside an open run block so edit payloads can contain examples:

```xml
<run>
cat <<'EOF' | src edit main.go
===BEFORE===
<run>
hello
</run>
===AFTER===
<run>
world
</run>
EOF
</run>
```

After the first closing run tag, all remaining text is dropped from
persistence, display, and execution. Non-whitespace dropped tail content is
logged as a warning.

An extra closing run tag before a run block opens is ignored. If the parser
reaches end of input while a run block is still open, it returns a diagnostic
instead of executing partial shell.

## Runtime Tags

Command observations are wrapped in result tags. Runtime-owned repairs and
notifications are wrapped in runtime tags. These tags are model-facing protocol
markers owned by `internal/agent/lenosbash`.

## Invalid Forms

Unclosed run blocks are invalid:

```xml
<run>
ls
```

Malformed bash inside a valid run block is invalid:

```xml
<run>
if true then
</run>
```

Fenced bash is Markdown prose, not executable bash:

````markdown
```bash
ls
```
````
