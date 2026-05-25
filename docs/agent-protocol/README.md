# Agent Protocol MOC

Lenos uses one agent output protocol: bash.

The model emits shell text. The runtime executes it, records the result in
SQLite, and either loops with a result observation or stops. Human-facing prose
is also bash: the model calls the injected `narrate` shell function with a
heredoc body.

## Core Documents

- [Protocol Pipeline](protocol-pipeline.html): a human-readable visual overview
  of the current protocol, processing pipeline, parser gates, and rewrite rules.
- [Context Compaction](context-compaction.html): pre-step auto compact,
  bash-first boundaries, and manual compact design.
- [Protocol](protocol.md): the bash-only contract and loop lifecycle.
- [Classifier](classifier.md): how an emit becomes exit, bash, prose rewrite,
  or a runtime correction.
- [Salvage](salvage.md): the narrow rewrite for "prose first line, valid bash
  after it."
- [Runtime Recovery](runtime-recovery.md): re-prompts for invalid shapes and
  failed execution.
- [Narrate IPC](narrate-ipc.md): how the bash function reports narration back
  to the Go runtime.
- [Storage and Rendering](storage-rendering.md): SQLite content shape, model
  replay, and TUI rendering.

## Implementation Map

- `internal/agent/classify.go`: emit classification and natural-language
  detection.
- `internal/agent/narrate.go`: `narrate` shell prelude, IPC event reading,
  addressed delivery, and heredoc rewrite helpers.
- `internal/agent/loop.go`: model loop, command execution, result persistence,
  and stop/continue rules.
- `internal/agent/prompt_runtime.go`: corrective runtime observations.
- `internal/agent/templates/system_prompt.tpl`: base prompt for the bash-only
  protocol.
- `internal/message/content.go`: `CommandContent`, `CommandNarration`, and
  result replay formatting.
- `internal/ui/chat/generic.go`: command result and narration rendering.
- `internal/ui/chat/assistant.go`: assistant emits render as bash previews.

## Design Rule

There is no second in-band markdown protocol. If a behavior cannot be expressed
as bash plus runtime-owned result metadata, it does not belong in the agent
emit format.
