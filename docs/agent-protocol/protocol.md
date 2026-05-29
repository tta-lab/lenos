# Protocol

The protocol is Lenos Bash: bash plus top-level `m` message blocks. Every
assistant response is parsed once. Message blocks are removed, the remaining
bash is executed with one `bash -c` call, and bare `exit` ends the loop without
execution.

## Valid Shapes

```bash
ls -la
```

```bash
# inspect before editing
cat README.md && rg "needle" .
```

```bash
m"Done. Tests pass."
```

```bash
m(reviewer)"Please review the auth change."
```

```bash
m"Reading the parser before editing."
rg "func Parse" internal/agent
```

```bash
exit
```

## Message Blocks

`m"..."` is natural language. It can be single-line or multi-line. Use
`m(target)"..."` to deliver the message to another agent. If `--pair-with` is
set, untargeted message blocks are also delivered to that default target;
explicit targets take precedence.

Message blocks must be top-level and begin at the start of their own physical
line, ignoring indentation. Text inside heredocs, shell strings, command words,
or comments is normal bash text, not speech.

## Loop Lifecycle

After parsing and optional bash execution:

- If only message blocks remain, publish them and stop the loop.
- If bash plus message blocks are present, run bash first. On exit code 0,
  publish the messages and continue or stop according to the normal result
  flow. On non-zero exit, persist the failed result and suppress the extracted
  messages.
- If exit code is 0 and there are no message blocks, send the command result
  back to the model and continue.
- If addressed message delivery fails, persist delivery status as a result row.
- If the model emits bare `exit`, stop the loop without executing bash.

## Natural-Language Safety Net

Raw prose is invalid. The runtime sends a diagnostic that points at the text
and asks for bash or a message block.
