# Protocol

The protocol is bash-only. Every assistant response is interpreted as shell
input and executed with one `bash -c` call, except for bare `exit`, which ends
the loop without execution.

## Valid Shapes

```bash
ls -la
```

```bash
# inspect before editing
cat README.md && rg "needle" .
```

```bash
narrate <<'EOF'
Done. Tests pass.
EOF
```

```bash
narrate --to reviewer <<'EOF'
Please review the auth change.
EOF
```

```bash
narrate --continue <<'EOF'
I found the parser and will patch it next.
EOF
```

```bash
exit
```

## Narration

`narrate` is not an external protocol marker. It is a bash function injected
by the runtime before the model's shell text is executed.

The function reads stdin and writes one IPC event. Positional arguments are not
message body text; `narrate "Done"` is invalid. Empty stdin is also invalid.
`--to <agent>` records an addressee for delivery through `ttal send`.
`--continue` records that this narration should not end the agent loop.
Options may be combined. Multiple `narrate` calls in one bash response are
allowed and render in event order.

The runtime does not inspect narration until the whole bash subprocess exits.
Commands after `narrate` still run.

## Loop Lifecycle

After bash exits:

- If exit code is 0 and at least one narration exists, render the narration
  and stop the loop unless any narration used `--continue`.
- If exit code is 0 and there is no narration, send the command result back to
  the model and continue.
- If exit code is non-zero, persist the failed result. If narration exists,
  render it after the failed command result, but continue the loop.
- If addressed narration delivery fails, persist delivery status and continue
  the loop with an observation that omits the narration body.
- If the model emits bare `exit`, stop the loop without executing bash.

## Natural-Language Safety Net

If an assistant response clearly looks like reader-facing prose, the runtime
rewrites it to a `narrate` heredoc, stores that rewritten assistant message,
executes it, and applies the same lifecycle rules above.

This safety net is for model mistakes. Prompts still instruct the model to
emit explicit bash.
