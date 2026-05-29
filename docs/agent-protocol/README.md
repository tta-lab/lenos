# Agent Protocol MOC

Lenos uses one agent output protocol: Lenos Bash.

Lenos Bash is bash plus `m` message blocks. The model emits one text response.
The runtime parses message blocks, executes the remaining shell text, records
results in SQLite, and either loops with a result observation or stops.

## Core Documents

- [Protocol](protocol.md): the Lenos Bash contract and loop lifecycle.
- [Message Blocks](message-blocks.md): `m` block syntax for speaking natural
  language from Lenos Bash.
- [Classifier](classifier.md): how an emit becomes exit, bash, message blocks,
  or a runtime correction.
- [Runtime Recovery](runtime-recovery.md): re-prompts for invalid shapes and
  failed execution.
- [Storage and Rendering](storage-rendering.md): SQLite content shape, model
  replay, and TUI rendering.

## Implementation Map

- `internal/agent/classify.go`: emit classification and natural-language
  detection.
- `internal/agent/loop.go`: model loop, command execution, result persistence,
  message delivery, and stop/continue rules.
- `internal/agent/prompt_runtime.go`: corrective runtime observations.
- `internal/agent/templates/system_prompt.tpl`: base prompt for the Lenos Bash
  protocol.
- `internal/message/content.go`: `CommandContent` and result replay formatting.
- `internal/ui/chat/generic.go`: command result rendering.
- `internal/ui/chat/assistant.go`: assistant emits render as bash previews.

## Design Rule

There is no second in-band markdown protocol. Human-facing language belongs in
message blocks. Everything else is shell.
