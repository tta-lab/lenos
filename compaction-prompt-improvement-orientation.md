# Compaction Prompt Improvement Orientation

## Goal

Improve Lenos's existing local compaction prompt so GPT/Codex/OpenAI models stop producing overly verbose session summaries with copied code, command transcripts, and low-value implementation narration.

This is a lower-effort improvement than full Codex native compact-history integration and should benefit all providers that use the current markdown summary flow.

## Problem

Current local summary output can be extremely verbose. Example:

```text
/home/neil/code/projects/tta-lab/too-verbos-summary-from-codex.md
```

Observed bad behavior:

- Summary becomes hundreds of lines.
- It replays commands that worked and failed.
- It copies code snippets and shell scripts.
- It includes irrelevant environment details.
- It reads like a full transcript/audit log rather than a handoff.
- GPT/Codex models follow the current prompt too literally.

## Root Cause

The current summary template tells the model to be exhaustive.

Relevant file:

```text
internal/agent/templates/summary.md
```

Problem phrases and sections:

```text
Be thorough.
No limit.
Err on the side of too much detail rather than too little.
Critical context is worth the tokens.
Include everything they'd need to continue without asking questions.
```

The template also asks for:

- Files read/analyzed.
- Commands that worked, exact commands with context.
- Commands that failed, what was tried and why.
- Environment details.
- Technical context in broad detail.

For GPT/Codex models, this is an instruction to dump the transcript.

## Current Lenos Flow

Local compaction is implemented in:

```text
internal/agent/agent_session.go
```

Key functions:

```go
func (a *sessionAgent) Summarize(...)
func buildCompactSummaryPrompt(...)
func summaryInstructionsPrompt(...)
func formatSummaryPrompt(...)
func summaryOutputProtocolPrompt(...)
```

Current prompt construction:

```go
prompt := fantasy.Prompt{fantasy.NewSystemMessage(systemPrompt)}
prompt = append(prompt, history...)
prompt = append(prompt, fantasy.NewUserMessage(buildCompactSummaryPrompt(...)))
```

Important constraint:

- Do not replace the system prompt with the summary prompt.
- The normal system prompt stays as prefix to preserve prompt-cache behavior.

So the easiest improvement is to edit the final compact-summary user instruction, especially `summary.md`.

## Upstream/Reference Findings

### Crush

Crush uses the same verbose `summary.md` template, but it uses it as the summarizer system prompt rather than putting the normal coder prompt first. Lenos intentionally keeps the normal system prompt for cache stability, so do not copy Crush's exact flow.

### Codex Local Prompt

Codex's local fallback compaction prompt is much shorter:

```text
You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary for another LLM that will resume the task.

Include:
- Current progress and key decisions made
- Important context, constraints, or user preferences
- What remains to be done (clear next steps)
- Any critical data, examples, or references needed to continue

Be concise, structured, and focused on helping the next LLM seamlessly continue the work.
```

This is a better direction for Lenos's local markdown summary path.

### OpenCode Prompt

OpenCode's prompt asks for a detailed but concise summary and focuses on:

- What was done.
- Current work.
- Files being modified.
- What needs to be done next.
- User constraints/preferences.
- Important technical decisions and why.

It explicitly says the summary should be comprehensive enough for context but concise enough to be quickly understood.

### Internal Breathe Skill

The local `breathe` skill is also a useful internal reference. It frames handoff as shedding conversation weight while keeping important state.

Useful structure from `skill get breathe`:

```text
## Active Task
## What Was Done
## Key Decisions
## Current State
## Next Steps
## Important Context
```

Useful rules:

- Self-contained; no "earlier in this conversation" references.
- Specific file paths.
- Decisions include the why.
- Next steps are actionable.
- Do not include full file contents.
- Do not include conversation history or back-and-forth.
- Do not include tool output or logs; summarize instead.
- Target: 50-200 lines.

This aligns with the desired behavior. But for `summary.md`, prefer Codex's simpler prompt shape over copying the full breathe template verbatim.

## Recommended Prompt Direction

Replace the current exhaustive `summary.md` with a concise handoff prompt.

Use Codex's simple structure as the main model, with `breathe` as the internal quality checklist.

Proposed behavior:

- Target a handoff, not a transcript.
- Keep decisions, constraints, files changed, current state, and next steps.
- Mention commands/tests only if they establish current state.
- Do not include command transcripts.
- Do not include code blocks unless a tiny snippet is essential.
- Do not list every file read.
- Do not list failed commands unless they still matter.
- Prefer bullets and short sections.
- Add a soft length budget.

## Proposed New `summary.md`

Draft:

```text
You are performing a CONTEXT CHECKPOINT COMPACTION. Create a concise handoff summary for another LLM that will resume this session.

The summary is the only context the next assistant will receive, so preserve task state, decisions, constraints, and exact next steps. Do not replay the transcript.

Include only information that helps the next assistant continue work:

## Current State

- User's current goal or request.
- What has been completed.
- What is in progress or unresolved.
- Important user preferences or constraints.

## Key Changes and Files

- Files changed and what changed.
- Files that are important for continuing.
- Important code locations when useful.

Do not list every file that was merely read.

## Decisions and Context

- Important technical decisions and why.
- Relevant architecture or provider behavior.
- Known risks, blockers, or assumptions.

## Verification

- Tests or checks that passed.
- Tests or checks that failed only if still relevant.
- Do not include full command output.

## Next Steps

List concrete next actions in order.

Rules:

- Be concise and structured.
- Target 400-900 words unless the session truly requires more.
- Do not include code blocks unless a short snippet is essential.
- Do not include shell transcripts, tool output, or copied source.
- Do not mention commands that were only exploratory unless their result matters.
- Prefer facts over narration.
```

## Secondary Code Change

`formatSummaryPrompt()` currently adds:

```go
Provide a detailed summary of our conversation above.
```

This should be softened to match the new prompt:

```go
Create a concise handoff summary of the conversation above.
```

If todos exist, keep todo status inclusion, but avoid expanding into transcript detail.

Relevant function:

```text
internal/agent/agent_session.go
func formatSummaryPrompt(todos []session.Todo) string
```

## Tests to Add or Update

Prompt text is data, not code. Do not add brittle unit tests that check exact prompt prose or scan for verbose phrases.

Prefer behavior and rendering-invariant tests around the compact prompt assembly.

Recommended tests:

1. `buildCompactSummaryPrompt(...)` still returns a non-empty final user instruction.

2. The compact prompt assembly preserves the required structure:
   - summary instructions are included before task/todo context.
   - todo context is included when subtasks exist.
   - output protocol is appended last.

3. `formatSummaryPrompt(todos)` still includes todo statuses and the task completion instruction when todos are provided.

4. `formatSummaryPrompt(nil)` does not include the todo section.

5. `summaryOutputProtocolPrompt()` still requires summary-only Markdown output and forbids wrapper formats such as JSON/XML/fences.

6. Existing `Summarize()` behavior remains intact:
   - normal system prompt stays as the first message for cache-prefix stability.
   - compact request is appended as the final user message.
   - summary message is still stored as an assistant summary row.

Avoid tests that assert exact wording such as "concise handoff" or forbid phrases such as "No limit". Those make prompt iteration harder without proving behavior.
## Verification Commands

Use:

```bash
go test ./internal/agent -count=1
go test ./... -count=1
```

If only prompt tests are touched, a narrower first run is acceptable:

```bash
go test ./internal/agent -run 'Test.*Summary|TestAgent_Summarize' -count=1
```

Then run full tests before committing.

## Expected Benefit

This should improve local compaction output for:

- Codex/GPT models.
- Anthropic models.
- Gemini/OpenRouter models.

It does not require provider-native raw history integration and can ship independently.

## Non-Goals

This does not implement official Codex `/responses/compact` replacement-history integration.

That separate path requires:

- Fantasy raw Responses input prefix support.
- Lenos storage for provider-native compacted history.
- Future Codex turns replaying encrypted `compaction_summary` items.

This prompt improvement is still valuable because non-Codex providers and non-native compact paths will continue to need markdown handoff summaries.

## Open Questions

1. What length target is best: 400-900 words, 1000 words, or line-based?
2. Should the prompt say "never include code blocks" or "only if essential"?
3. Should failed commands be completely forbidden or allowed if they are still a blocker?
4. Should we preserve the exact required sections, or allow a more flexible handoff shape?
5. Should GPT/Codex get provider-specific stricter wording, or should one prompt serve all models?
