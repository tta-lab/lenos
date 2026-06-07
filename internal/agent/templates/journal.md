# Session Journal

## Task

- User request:
- Working goal:
- Done means:

## Preflight

When the runtime says no project guidance was found, run at least one `ei ask`
before changing files. The helper inspects the workspace read-only — you still
decide, update the journal, and own the task.

Choose the prompt that fits the task. Provide the full task text.

**Constraints**: find important constraints, forbidden changes, required outputs,
and wording that is easy to miss.

```bash
cat <<'PROMPT_EOF' | ei ask
Inspect the current workspace for this task. Find important constraints,
forbidden changes, required outputs, and wording that is easy to miss.

Task:
<PASTE TASK HERE>
PROMPT_EOF
```

**Verification**: find available verification paths — tests, scripts, expected
output files, task metadata, or commands that can be run before final answer.

```bash
cat <<'PROMPT_EOF' | ei ask
Inspect the current workspace for this task. Find available verification paths:
tests, scripts, expected output files, task metadata, verifier hints, or commands
that can be run before final answer.

Task:
<PASTE TASK HERE>
PROMPT_EOF
```

**Safest approach**: ask for the smallest change that can satisfy the task, with
likely pitfalls.

```bash
cat <<'PROMPT_EOF' | ei ask
Inspect the current workspace and propose the safest first approach. Focus on
local files, existing tools, likely pitfalls, and the smallest change that can
satisfy the task.

Task:
<PASTE TASK HERE>
PROMPT_EOF
```

**Preflight review**: validate a filled Preflight draft for gaps.

```bash
cat <<'PROMPT_EOF' | ei ask
Review this preflight draft for missing constraints, missing verification paths,
or risky assumptions. Suggest corrections only.

Task:
<PASTE TASK HERE>

Preflight draft:
<PASTE DRAFT HERE>
PROMPT_EOF
```

Use the helper's answer as input only. You are still responsible for deciding what
to do, updating this journal yourself, and completing the task.

## Context

- Project facts:
- Important files:
- User/system constraints:
- Non-goals:

## Environment

- Working directory:
- Runtime/tooling facts:
- Sandbox/env limits:
- Persistence traps:

## Deliverables

- Required artifacts:
- User-visible outcomes:
- Acceptance criteria:

## Existing Verification

- Tests/checks that already pass:
- What the existing verification proves:
- Gaps in existing coverage:

## Potential Delivery Risks

- What could still go wrong:
- Unverified assumptions:
- Environment/scoping gaps:

## Plan

- Current plan:
- Approach:

## Progress

- Done:
- Decisions:
- Failed paths:

## Verification

- Commands/checks run:
- Results:
- Remaining proof needed:

## Reflection

- What worked:
- What didn't:
- What to do differently next time:

## Next

-
