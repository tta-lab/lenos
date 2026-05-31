# Upstream v0.55.0→v0.74.1 — Commit Review

> Detailed per-category reviews of the ~350 relevant upstream commits
> (after excluding deleted features: server, client, backend, proto,
> swagger, lsp, permission, shell, tools; and provider-specific:
> oauth, copilot, hyper).

## Categories

| File | Priority | Commits | Focus |
|---|---|---|---|
| [DB/sqlc](v0.74-db.done.md) | **HIGH** ✅ | 11 | Pool, corruption, lock, migration |
| [Agent/Loop](v0.74-agent.md) | **MEDIUM** | ~25 | Token accounting, race fixes, env hardening |
| [Config](v0.74-config.md) | **HIGH** | ~20 | Path discovery, model fallback, OAuth, expansion |
| [UI/Perf](v0.74-ui.md) | **HIGH** | ~87 | Chat rendering, caching, scrollbar, dialogs |
| [Pubsub](v0.74-pubsub.done.md) | **MEDIUM** ✅ | 9 | Buffer size, dropped events, batching |
| [Hooks](v0.74-hooks.md) | **MEDIUM** | 6 | Matcher, env vars, runner |
| [Other](v0.74-other.md) | **LOW** | ~15 | Notifications, fsext, prompts, providers |

## Quick merge checklist (top 7)

1. `6938dedd` — Perf: batch streaming updates → DB/UI load
2. `c2be8cbf` — Agent: `EDITOR=false PAGER=cat` → prevent hangs
3. `9d346688` — Agent: release activeRequests → spinner stuck
4. `2faa467a` — Agent: reasoning_effort only when supported
5. `77b6c38b` — UI: render only visible lines → perf
6. `6b101f38` — UI: skip unchanged items → perf
7. `e2e0bc09` — Config: scope discovery to repo boundary
