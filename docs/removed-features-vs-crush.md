# Removed upstream features (Crush v0.55.0 had, Lenos HEAD dropped)

This list only covers features that were **present in upstream Crush v0.55.0**
— NOT Lenos-only additions (narrate, ei agents, transcript, project resolution,
LENOS\_\* env vars). Those never existed upstream so v0.74 commits about them
don't need tracking.

Purpose: when scanning upstream v0.55→v0.74 commits, changes touching these
features can be skipped — they'd hit dead code or cause conflicts.

---

## 1. Tool-Call protocol (REMOVED)

Upstream Crush v0.55.0 used XML `<tool>` blocks dispatching native Go tools
(read/write/edit/search/grep/bash). Lenos replaced with bash-first: everything
goes through `<bash>` blocks + bash commands.

Skip: all tool-call layer commits in v0.74.

---

## 2. HTTP Server / Client-Server architecture (REMOVED)

Upstream had a full client-server split: `internal/server/`, `internal/client/`,
`internal/backend/`, `internal/proto/`, `internal/swagger/`, `ClientWorkspace`,
`--yolo` flag, `LENOS_CLIENT_SERVER` env var. Lenos deleted all 45 files.
Lenos runs local-only.

Skip: all client/server/backend/proto/swagger commits in v0.74.

---

## 3. LSP (REMOVED)

Upstream had `internal/lsp/` (client, manager, diagnostics, config). Lenos
deleted all LSP code.

Skip: all `internal/lsp/` commits in v0.74.

---

## 4. Permission system (REMOVED)

Upstream had `internal/permission/` — approval prompts, command-blocking rules,
`allowed_tools` config, permission dialog UI. Lenos deleted all. Sandboxing
is now enforced by temenos at the OS level.

Skip: all `internal/permission/` + `allowed_tools` commits in v0.74.

---

## 5. Per-session Todos (REMOVED)

Upstream stored todos as a SQLite JSON blob (`todos` column on sessions table).
Lenos replaced with live taskwarrior subtask queries.

Skip: all `todos` column / `session.Todos` commits in v0.74.

---

## 6. mvdan/sh dependency (REMOVED)

Upstream used `mvdan/sh` for shell parsing and command building
(`internal/shell/` package). Lenos replaced with `exec.CommandContext(ctx,
"/bin/bash", "-c", bash)`. Also removed `fsext.Expand`, `ExecShell`,
`BackgroundShellManager`.

Skip: all `internal/shell/` / mvdan/sh commits in v0.74.

---

## 7. Multi-Edit tool (REMOVED)

Upstream had a `multiedit` tool for batch editing. Lenos deleted it — agents
use `src edit` (single-edit, atomic).

Skip: all multiedit commits in v0.74.

---

## 8. Title generation (CHANGED — upstream uses LLM, we use taskwarrior)

Upstream `generateTitle()` made a separate LLM call from the user prompt.
Lenos extracts the task description from `task export`.

Evaluate: upstream LLM prompt improvements may still be useful.

---

## Same feature names, different implementations (CHANGED)

| Feature | Upstream | Lenos | Mergeable commits |
|---|---|---|---|
| Background Jobs | built-in job tools | temenos job socket polling | **no** (different backend) |
| Skills | SKILL.md discovery | same discovery paths, different integration | **only** SKILL.md format changes |
| Prompt Templates | Go templates | Go templates but content completely rewritten | **no** (bash-first prompt is different) |

---

## Commit filter summary

When scanning upstream v0.55→v0.74, skip commits touching:

| Pattern | Reason |
|---|---|
| `internal/server/`, `internal/client/`, `internal/backend/` | HTTP server deleted |
| `internal/proto/`, `internal/swagger/` | protocol/swagger deleted |
| `internal/lsp/` | LSP deleted |
| `internal/permission/` | permissions deleted |
| `internal/shell/` | mvdan/sh deleted |
| `todos` column / `session.Todos` | per-session todos deleted |
| `multiedit` / `multi_edit` | multi-edit deleted |
| `internal/agent/tools/` (except bash.go) | native tools deleted |
| `--yolo` flag | deleted |
| `ClientWorkspace` | deleted |
| `allowed_tools` config field | deleted |
