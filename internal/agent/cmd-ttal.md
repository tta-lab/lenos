Manage tasks, send messages, and control the ttal agent runtime

## Tasks
  ttal task add "description" --project <alias> --tag <tag> --priority M
  ttal task get              # load full task context and plan
  ttal task find "keyword"   # search pending tasks
  ttal task find "keyword" --completed  # search completed tasks
  ttal go <uuid>            # advance task through pipeline stage

## Messaging
  ttal send --to <agent> "message"              # send to agent
  ttal send --to <job_id>:<agent> "message"    # send to worker

## Projects & Agents
  ttal project list         # all active projects with paths
  ttal agent list           # all registered agents
  ttal agent info <name>    # agent details

## Today
  ttal today list           # tasks scheduled for today
  ttal today add <uuid>     # schedule task for today
  ttal today completed      # completed today

## PRs
  ttal pr create "title" --body "description"
  ttal pr modify --title "new"
  ttal go <uuid>            # squash merge (after LGTM)

## Voice
  ttal voice speak "text"   # speak with your assigned voice
  ttal voice speak "text" --voice <id>  # specific voice
  ttal voice status         # check voice server health

## Sync
  ttal sync                # deploy agents + config to runtime dirs
  ttal sync --dry-run       # preview what would be deployed

## Tips
- Always use `ttal task get` (no extra params) to load task context at session start.
- Use `ttal go <uuid>` to route tasks to agents — don't do everything yourself.
- Reply naturally to human messages — the bridge delivers your text to Telegram.
