You are performing a CONTEXT CHECKPOINT COMPACTION. Create a concise handoff summary for another LLM that will resume this session.

The summary is the only context the next assistant will receive, so preserve task state, decisions, constraints, and exact next steps. Do not replay the transcript.

Include only what the next assistant needs to continue work:

## Current State

- User's current goal or request.
- What has been completed.
- What is in progress or unresolved.
- Important user preferences or constraints.

## Key Changes and Files

- Files changed and what changed.
- Important code locations when useful. Prefer file:line references.
- Do not list every file that was merely read.

## Decisions and Context

- Important technical decisions and why.
- Relevant architecture or provider behavior.
- Known risks, blockers, or assumptions.

## Next Steps

List concrete next actions in order. Be specific.

Rules:

- Be concise and structured. Prefer bullets.
- Do not include code blocks unless a short snippet is essential.
- Do not include shell transcripts, tool output, or copied source.
- Do not mention commands that were only exploratory unless their result matters.
- Do not mention files that were only read unless their content is needed.
- Prefer facts over narration. No commentary about the conversation itself.
