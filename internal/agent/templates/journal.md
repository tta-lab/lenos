# Session Journal

## Task

> Capture the user's request in plain words. Keep this to the actual task, not
> commentary about the session.

- User request:
- Working goal:

## Goal

> Define what done means before choosing an approach. Include evidence that
> would prove completion.

- Success state:
- Non-goals:
- Completion evidence:

## Context

> Record durable project facts the agent should not rediscover. Include only
> facts that affect this task.

- Project:
- Important files:
- Constraints:

## Constraints

> List hard limits and safety rules. Include time, security, sandbox, user, and
> runtime constraints.

- User constraints:
- System/runtime constraints:
- Security/privacy constraints:
- Time/budget constraints:

## Environment

> Inspect the environment before planning. Record tools and runtimes that change
> what approach is practical. Check for mature domain libraries before writing
> custom algorithms.

- Working directory:
- OS/container notes:
- Package managers:
- Language runtimes:
- Important PATH tools:
- Mature domain libraries available (numerical, parsing, compilation, etc.):

## Existing Verification

> Find tests, verifiers, or smoke checks before editing. If none are known, say
> what is unknown. Read the assertions, not just that a test exists—what exact
> files, paths, exit codes, output format, or state does it require?

- Test commands:
- Verifier commands:
- What exact assertions do they make (files, exit codes, output format, state)?
- Smoke checks:
- Unknowns:

## Deliverables

> Name the final files, commands, services, or state the task requires. Include
> how to check both existence and correctness. Identify who or what will consume
> the output and from what runtime path.

- Required final files/state:
- Consumer runtime and path (who will use this, and from where?):
- Local checks to prove them:
- Semantic checks, not just existence:

## Potential Delivery Risks

> Predict how this task could fail even after useful work. Use this section to
> avoid late surprises and repeated dead ends.

- Missing tools or dependencies:
- Hidden verifier expectations:
- Environment isolation traps (venv, PYTHONPATH, --target installs, local env
  vars that the consumer won't see):
- Environment state that may not persist:
- Background process lifecycle (will the service survive past the commands
  that started it?):
- Long-running/background process risks:
- Paths likely to waste time:

## Plan

> Keep the plan short and update it when the approach changes.

- 

## Failed Paths

> Record approaches that failed and why. This prevents repeating work without
> new evidence.

- 

## Decisions

> Record choices that affect future work. Include why the choice was made.

- 

## Progress

> Record durable progress only. Do not log every command.

- 

## Verification

> Record verification commands and results. Distinguish existence checks from
> semantic checks.

- 

## Reflection

> After the task is complete, record what would help next time. Keep this brief
> and forward-looking—patterns, not narration.

- What went well:
- What could have gone better:
- Pattern worth remembering:

## Next

> State the next concrete action. Use `none` only when the task is complete.

-
