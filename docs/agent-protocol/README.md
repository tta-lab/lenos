# Agent Protocol MOC

Lenos uses one agent output protocol: Lenos Bash.

Lenos Bash is Markdown prose plus tagged bash blocks. The model emits one text
response. The runtime renders Markdown outside tags, executes each parsed bash
block, records results in SQLite, and either loops with a result observation or
stops.

## Core Documents

- [Protocol](protocol.md): the Lenos Bash contract and loop lifecycle.
- [Bash Tags](message-blocks.md): tagged bash syntax and parser edge cases.
- [Classifier](classifier.md): how an emit becomes exit, bash, Markdown prose,
  or a runtime correction.
- [Runtime Recovery](runtime-recovery.md): re-prompts for invalid shapes and
  failed execution.
- [Storage and Rendering](storage-rendering.md): SQLite content shape, model
  replay, and TUI rendering.

## Implementation Map

- `internal/agent/classify.go`: emit classification and natural-language
  detection.
- `internal/agent/loop.go`: model loop, command execution, result persistence,
  and stop/continue rules.
- `internal/agent/prompt_runtime.go`: corrective runtime observations.
- `internal/agent/templates/system_prompt.tpl`: base prompt for the Lenos Bash
  protocol.
- `internal/message/content.go`: `CommandContent` and result replay formatting.
- `internal/ui/chat/generic.go`: command result rendering.
- `internal/ui/chat/assistant.go`: assistant emits render as bash previews.

## Design Rule

There is no second message protocol. Human-facing language is plain Markdown
outside bash blocks. Executable work belongs inside bash blocks.
