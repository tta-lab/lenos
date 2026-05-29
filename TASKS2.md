# TASKS2: Integrate Message Blocks Into Lenos Runtime

## Goal

Use the parser from TASKS1 in the Lenos agent loop so assistant emits can mix
Lenos message blocks and bash while preserving one bash subprocess per emit.

## Dependency

Do not start until TASKS1 and TASKS1.1 are complete.

Current parser status:

- `sh` PR #2 is capped and pushed at `0fd5444c`.
- Consume fork tag `v3.13.1-lensh.1`, which points at `0fd5444c`.
- Expected durable dependency shape, if Go accepts it:
  `replace mvdan.cc/sh/v3 => github.com/tta-lab/sh v3.13.1-lensh.1`.
- Do not commit an absolute local `replace` to `/home/neil/code/projects/tta-lab/sh`.
- If module resolution for the fork is unclear, make that the first worker
  deliverable and stop before runtime wiring.

## References

- `flicknote://note/09b84178-47fe-49dc-bb40-a3ab62c2bd84`
- `flicknote://note/74688c39-dde4-4333-92d2-5330f99e0f22`
- `flicknote://note/ca4e2e09-f70d-4cfe-a9d2-c066df03a9a3`
- `flicknote://note/202ae86e-ed37-4497-85c2-299618cb6403`

## Runtime Rules

1. Parse assistant emit as Lenos Bash before existing bash classification.
2. Extract line-start top-level `m` message blocks via `syntax.ScanMsgBlocks`.
3. Validate the cleaned bash with mvdan syntax parsing before existing
   `bash -n` fallback/classification.
4. If parser returns a message-block protocol error, re-prompt with a
   diagnostic and do not run bash.
5. If bash remains, execute it once with the existing runner.
6. If bash succeeds, publish extracted messages in source order.
7. If bash fails, publish no extracted messages and continue normal failure
   recovery.
8. If no bash remains, publish message blocks directly.
9. Lifecycle:
   - message-only `m` ends the loop;
   - mixed bash plus messages follows existing bash result flow; message blocks
     do not end the loop.

Important protocol distinction:

- `echo ok; m"done"` / `cmd & m"done"` / `cmd | m"done"` are invalid Lenos
  Bash and should become model repair prompts.
- `cat <<EOF\nm"literal"\nEOF` is valid bash with literal heredoc content and
  should not emit a message or error.

## Storage And Rendering

Decide and implement storage shape:

- Preserve original assistant source for audit/history if possible.
- Store extracted message events structurally so UI can render them as
  narration/prose.
- Store remaining bash as the command that actually ran.

Keep the UI behavior consistent with existing narration display.

## Classifier Changes

- Raw natural language should remain invalid or be rewritten only by the
  existing safety net until prompt work lands.
- Message blocks should not go through `bash -n` as raw shell.
- Same-line misplaced message blocks should not fall through to
  command-not-found.
- Existing banned-pattern and tool-call checks should still apply to remaining
  bash.
- Keep command-not-found and invalid-bash repair paths.

Suggested implementation order:

1. Add an internal adapter package, for example
   `internal/agent/lenosbash`, that wraps `mvdan.cc/sh/v3/syntax`.
2. Adapter returns:
   - original source;
   - cleaned bash;
   - extracted message blocks;
   - `HasBash`;
   - structured diagnostic.
3. Update `classify`/loop only after adapter tests pass.
4. Keep narrate behavior untouched until message-block behavior is stable.

## Tests

Add focused tests for:

- message-only `m` stops;
- message-only multiple `m` blocks publish in source order and then stop;
- mixed bash plus `m` continues according to bash result flow;
- mixed bash success publishes messages;
- mixed bash failure suppresses messages;
- addressed message delivery success/failure;
- parser placement errors surface clearly;
- same-line `m` after `;`, `&`, `|`, and heredoc setup produces repair prompt;
- heredoc-body `m"..."` remains literal content and does not publish;
- old `narrate` behavior still works if retained.

## Acceptance

- `go test ./internal/agent ./internal/message ./internal/ui/...` or the
  relevant focused package set passes.
- No response requires more than one bash subprocess because of message blocks.
- Failure semantics prevent false success messages.
- Existing bash-only agent behavior is not regressed.

## Handoff

After this lands, TASKS3 can update config/prefill and TASKS4 can update the
system prompt.
