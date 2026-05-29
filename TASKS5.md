# TASKS5: Remove Legacy Narrate After Message Blocks Stabilize

## Goal

Remove the old `narrate` response path after the `m` message block protocol is
fully wired, documented, and taught to models.

This is a cleanup phase, not part of initial message-block rollout. The target
state is one primary way for agents to speak natural language: `m` message
blocks in Lenos Bash.

## Dependency

Start only after these are merged and verified:

- TASKS2: runtime message-block integration.
- TASKS2.1: structured diagnostics.
- TASKS3: optional `m` assistant prefill.
- TASKS4: system prompt update that teaches `m` and stops teaching `narrate`.

Do not start this task while the main prompt still asks models to use
`narrate`, or while context compaction/internal flows still require it.

## References

- `flicknote://note/09b84178-47fe-49dc-bb40-a3ab62c2bd84`
- `flicknote://note/74688c39-dde4-4333-92d2-5330f99e0f22`
- `flicknote://note/ca4e2e09-f70d-4cfe-a9d2-c066df03a9a3`
- `flicknote://note/202ae86e-ed37-4497-85c2-299618cb6403`

## Scope

Remove or downgrade legacy narrate behavior:

- Natural-language auto-rewrite to `cat <<... | narrate`.
- Main-loop special handling that depends on `narrate` as the user-facing prose
  path.
- Model-facing prompt/docs that teach `narrate` as normal response syntax.
- Tests whose only purpose is preserving old natural-language-to-`narrate`
  behavior.

Keep any low-level narration storage/rendering types that are still used by
message blocks. The user-visible cleanup is about removing `narrate` as an
agent response protocol, not deleting useful UI/storage primitives.

## Preflight Audit

Before deleting code, audit current uses:

```bash
rg -n "narrate|Narration|CommandNarration|narrateCommandForBody" internal docs
```

Classify each use:

- message-block runtime/storage/rendering: keep or rename only if useful;
- legacy model response protocol: remove;
- context compaction/internal control flow: migrate first, then remove;
- docs/goldens: update to `m` examples.

If compaction still emits `narrate`, update compaction to use message blocks or
explicitly split this task into "remove prompt/runtime response narrate" and
"remove compaction narrate" phases.

## Runtime Rules After Cleanup

- Raw natural language is invalid Lenos Bash and should receive a repair prompt
  telling the model to use `m`.
- `m` message blocks are the only natural-language response syntax.
- Pure bash remains valid.
- Bash comments remain private work notes.
- Message-only `m` ends the loop.
- Mixed bash plus `m` follows normal bash result flow.
- Legacy `narrate` should not be suggested in repair prompts.

## Tests

Update or add focused tests for:

- raw natural language re-prompts to use `m`, not `narrate`;
- markdown/fenced prose re-prompts to use `m`, not `narrate`;
- old natural-language auto-rewrite test is removed or inverted;
- message-only `m` still stops;
- mixed bash plus `m` still runs one subprocess;
- addressed `m(to)` delivery still works;
- context compaction still works after any migration;
- system prompt golden files contain no normal-response `narrate` examples.

## Acceptance

- No model-facing prompt tells agents to use `narrate` for normal speech.
- Raw prose no longer gets rewritten to a `narrate` heredoc.
- Message-block behavior from TASKS2 remains green.
- Context compaction and any internal flows still pass tests.
- `go test ./internal/agent ./internal/message ./internal/ui/...` passes, plus
  any package touched by compaction or prompt changes.

