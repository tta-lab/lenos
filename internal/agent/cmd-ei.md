Ask questions and run agents via the einai runtime

## Ask
Ask a question — saves to flicknote with --save:
  ei ask "how does routing work?" --project myapp
  ei ask "what is this?" --url https://docs.example.com
  ei ask "latest Go generics syntax?" --web
  ei ask "summarize this project" --save

Async (runs in background, notifies on completion):
  ei ask "explain this code" --async --project myapp

## Run Agent
  ei agent run coder "implement the auth module"
  ei agent run coder "$(cat plan.md)"   # pipe from stdin

Async:
  ei agent run coder "task description" --async

## List & Daemon
  ei agent list           # available agents
  ei daemon status        # check daemon health
  ei daemon run           # start daemon in foreground

## Tips
- Use --async for long-running queries — pueue handles background execution.
- Results saved to ~/.einai/outputs/<runtime>/ with .md extension.
- Daemon socket: ~/.einai/daemon.sock
