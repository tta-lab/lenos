# Protocol

The protocol is Lenos Bash: Markdown prose plus one tagged bash block. Every
assistant response is parsed once. Markdown outside bash tags is reader-facing
prose only before the bash block. The parsed bash block is syntax-checked,
executed, and replayed to the model as a result observation. Bare `exit` ends
the loop without execution.

## Valid Shapes

```markdown
Done. Tests pass.
```

```xml
<bash>
rg "func Parse" internal/agent
</bash>
```

```xml
Reading the parser before editing.

<bash>
# inspect before editing
cat README.md && rg "needle" .
</bash>
```

```text
exit
```

## Bash Blocks

Only text between bash tags is executable. Markdown prose may appear before the
bash block. Bash start and end tags are recognized only when they begin at
column 1 on their own physical lines. Inline or indented tag text is plain
text. After the first bash end tag, the runtime drops all remaining text from
persistence, display, and execution, with a warning for non-whitespace tail
content. Bash tags inside an open bash block are treated with stack depth so
heredocs and edit payloads can contain literal tagged examples.

## Loop Lifecycle

After parsing and optional bash execution:

- If no bash blocks are present, render the Markdown prose and stop the loop.
- If a bash block is present, execute it.
- If a command exits 0, send the command result back to the model and continue.
- If a command exits non-zero, persist the failed result and continue with the
  normal failure recovery flow.
- If the model emits bare `exit`, stop the loop without executing bash.

## Natural-Language Safety Net

Raw prose is valid Markdown. It ends the current turn when no bash block is
present.
