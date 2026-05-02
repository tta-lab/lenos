Manage tasks and PRs via the ttal agent runtime

## Subtasks (taskwarrior)
  task <uuid> done           # mark a completed subtask
  task <uuid> start          # mark a subtask as in-progress
  task <uuid> annotate '<note>'  # add a note to a subtask

## Alerts (CRITICAL)
  ttal alert "message"       # escalate blockers to the planner — routes to owner agent, falls back to Telegram

## Comments & Status
  ttal comment add "message" # post progress updates, triage reports, findings

## PRs
  ttal pr create "title" --body "description"
  ttal pr modify --title "new" --body "description"
  ttal go                   # squash merge after LGTM (no extra params)

## Git
  ttal push                 # push current branch to origin — ALWAYS use this, never git push
