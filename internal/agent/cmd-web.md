Search the web and fetch web pages

## Search
  web search "golang context timeout patterns"
  web search "RFC 7231 HTTP semantics"

## Fetch

Fetch a web page — two-step navigation:

1. Show heading tree to find sections:
     web fetch https://docs.example.com/api
     web fetch https://docs.example.com/api --tree

2. Read a specific section by ID:
     web fetch https://docs.example.com/api -s 3f

Flags:
  --tree-threshold <n>   auto-tree above this char count (default: 5000)
  -s <id>                read section by 2-char heading ID
  --full                 full content, skip heading tree

## Library Docs (Context7)
  web docs resolve "effect-ts"       # discover doc IDs for a library
  web docs fetch /effect-ts/website "Stream"  # fetch a specific doc section

## Sourcegraph Code Search
  web sgraph "lang:go repo:^github\.com/golang/go$ context.WithTimeout"
  web sgraph "lang:typescript type:symbol useReducer"
  web sgraph "file:Dockerfile FROM golang" --count 20

## Tips
- Search results are ranked by relevance. Use quotes for exact phrases.
- For API docs, fetch the page then use -s to read specific sections.
