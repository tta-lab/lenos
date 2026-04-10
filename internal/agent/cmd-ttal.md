Manage tasks and PRs via the ttal agent runtime

## Session Start
  ttal task get              # load full task context and plan (no extra params)

## Subtasks
  task <uuid> done           # mark a completed subtask
  task <uuid> start          # mark a subtask as in-progress
  task <uuid> annotate '<note>'  # add a note to a subtask

## Alerts (CRITICAL)
  ttal alert "message"       # escalate blockers to the planner — triggers Telegram notification

## Comments & Status
  ttal comment add "message" # post progress updates, triage reports, findings

## PRs
  ttal pr create "title" --body "description"
  ttal pr modify --title "new" --body "description"
  ttal go                   # squash merge after LGTM (no extra params)

## Git
  ttal push                 # push current branch to origin — ALWAYS use this, never git push
