Ask questions via the einai runtime

## Ask
Ask a question — saves to flicknote with --save:
  ei ask "how does routing work?" --project myapp
  ei ask "what is this?" --url https://docs.example.com
  ei ask "latest Go generics syntax?" --web
  ei ask "summarize this project" --save

Async (runs in background, notifies on completion):
  ei ask "explain this code" --async --project myapp

## Tip
Use --async for long-running queries — runs in background, notifies on completion.
