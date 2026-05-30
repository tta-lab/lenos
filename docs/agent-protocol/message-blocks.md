# Bash Tags

Tagged bash blocks are the executable form for Lenos Bash. They let an
assistant response mix Markdown prose and shell while preserving one protocol:
Markdown outside tags, bash inside tags.

This document is parser-facing. The tag constants and render helpers live in
`internal/agent/lenosbash`; prompts and tests should use those helpers instead
of spelling protocol tags directly.

## Contract

Only text inside a bash block is executable. Text outside bash blocks is
Markdown prose.

If bash remains after parsing, Lenos runs each bash block in source order. If a
bash block fails parser validation, the runtime sends a syntax diagnostic and
does not execute it.

If no bash blocks are present, Lenos renders the Markdown prose and ends the
loop.

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

An extra closing bash tag when no bash block is open is ignored. If the parser
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
