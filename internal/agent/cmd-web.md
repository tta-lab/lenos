Search the web and fetch web pages

## Search
  web search "golang context timeout patterns"
  web search "RFC 7231 HTTP semantics"

Uses Exa (EXA_API_KEY), Brave Search (BRAVE_API_KEY), or DuckDuckGo (fallback).

## Fetch
Fetch a web page as markdown with heading navigation:
  web fetch https://docs.example.com/api
  web fetch https://docs.example.com/api -s cD      # read section by 2-char ID
  web fetch https://example.com --full               # full content, skip auto-tree
  web fetch https://example.com --tree               # show heading tree

Flags:
  --tree-threshold <n>   auto-tree above this char count (default: 5000)
  -s <id>                read section by heading ID
  --full                 full content, skip heading tree

Long pages (>5000 chars) auto-show a heading tree. Use -s to read specific sections.

## Tips
- Search results are ranked by relevance. Use quotes for exact phrases.
- Fetched pages are cached daily (unless BROWSER_GATEWAY_URL is set).
- For API docs, fetch the full page with --full then search within it.
