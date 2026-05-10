# Agent Protocol MOC

This directory documents the bash-first agent protocol and the runtime logic
that supports it.

Read in this order:

1. [Protocol](protocol.md) - surface protocol and core invariants.
2. [Classifier](classifier.md) - emit classes and classification order.
3. [Salvage](salvage.md) - the narrow prose-prefixed bash rewrite.
4. [Runtime Recovery](runtime-recovery.md) - malformed emit re-prompts.
5. [Storage And Rendering](storage-rendering.md) - database and TUI behavior.

## Short Version

Lenos does not rely on traditional model tool calls in the agent loop. The
assistant emits text. The runtime classifies that text as bash, markdown,
exit, or a retryable protocol error.

The runtime intentionally favors false negatives over false positives. Missing
a bash salvage stops the loop with markdown. Wrongly salvaging prose executes
text the model meant as communication, which is worse.

## Implementation Map

- `internal/agent/classify.go`: emit classification, natural-language rule,
  bash syntax check, prose-prefixed bash salvage.
- `internal/agent/loop.go`: creates assistant/result rows, applies
  classification outcomes, routes `:md`, executes bash.
- `internal/agent/salvage_probe.go`: runner-backed command/path probes for the
  salvage gate.
- `internal/agent/md_helpers.go`: `:md` prefix stripping, addressee parsing,
  trailing lifecycle marker stripping.
- `internal/agent/prompt_runtime.go`: runtime re-prompts for malformed emits.
- `internal/ui/chat/messages.go`: message extraction and TUI rendering choices.
