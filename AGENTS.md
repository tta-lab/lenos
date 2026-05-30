# Lenos Development Guide

## Project Overview

Lenos is a terminal-based AI coding assistant and interactive runtime for the
[ttal](https://github.com/tta-lab) ecosystem. Built in Go, it connects to LLMs
and gives them tools to read, write, and execute code. It supports multiple
providers (Anthropic, OpenAI, Gemini, Bedrock, Copilot, Hyper, MiniMax,
Vercel, and more) and supports extensibility via agent skills.

The module path is `github.com/tta-lab/lenos`.

## Architecture

```
main.go                            CLI entry point (cobra via internal/cmd)
internal/
  app/app.go                       Top-level wiring: DB, config, agents, events
  cmd/                             CLI commands (root, run, login, models, stats, sessions)
  config/
    config.go                      Config struct, context file paths, agent definitions
    load.go                        config.json loading and validation
    provider.go                    Provider configuration and model resolution
  agent/
    agent.go                       Types, NewSessionAgent, package doc
    agent_run.go                  Run + bash-first loop + recorder integration
    loop.go                       Bash-first agent loop (subprocess-per-call, exit intercept, step cap)
    classify.go                   Emit classifier (exit/empty/invalid/banned/exec)
    exec.go                       Runner interface + LocalRunner / SandboxRunner
    prompt_runtime.go             Re-prompt strings for invalid emits
    system_prompt.go              Bash-first base system-prompt builder + CommandDoc
    agent_session.go              Session ops + helpers (Summarize, Cancel, etc.)
    coordinator.go                 Coordinator: manages named agents ("coder", "task")
    prompts.go                     Loads Go-template system prompts
    templates/                     System prompt templates (coder.md.tpl, system_prompt.tpl, initialize.md.tpl, summary.md, cmd-git.tpl)
  session/session.go               Session CRUD + Todo persistence backed by SQLite (model SSOT per bash-first orientation 7015e7aa §4)
  (removed - DB as SSOT for session storage)
  message/                         Message model and content types
  db/                              SQLite via sqlc, with migrations
    sql/                           Raw SQL queries (consumed by sqlc)
    migrations/                    Schema migrations
  ui/                              Bubble Tea v2 TUI (see internal/ui/AGENTS.md)
  permission/                      Tool permission checking and allow-lists
  event/                           Telemetry (PostHog)
  pubsub/                          Internal pub/sub for cross-component messaging
  filetracker/                     Tracks files touched per session
  history/                         Prompt history
```

## Configuration & Models

The config store has two distinct model-update paths with different intent:

### `SetActiveModel` (in-memory only)

`Store.SetActiveModel(modelType, model)` updates the in-memory model map
only. It NEVER writes to disk, NEVER updates the recent-models list, and
NEVER leaks state to future sessions. Use this for session-ephemeral
selection: TUI dialog actions (model picker, thinking toggle, reasoning
effort) and CLI flag overrides (`-m`, `--small-model`).

The `Workspace` interface exposes only `SetActiveModel` — TUI code has no
access to the persistent path through this abstraction. If you find yourself
reaching for a persistent write from a TUI handler, you are re-introducing
the state-leakage bug.

### `UpdatePreferredModel` (persistent)

`Store.UpdatePreferredModel(scope, modelType, model)` writes to
`config.json` AND records the model in recent-models. Use this for explicit
persistence: load-time recovery (`configureSelectedModels`) and the
`lenos config set-model` subcommand.

`UpdatePreferredModel` is reachable only via direct store access (from
`internal/cmd/config.go`, not through the `Workspace` interface).

### Active-tier dispatch

The coder turn dispatches on `RuntimeOverrides.ActiveTier`:

- `ActiveTier == Large` (default) → primary = LargeModel from
  `cfg.Models[Large]`
- `ActiveTier == Small` (set via `--small-model` flag) → primary =
  SmallModel from `cfg.Models[Small]`

`agent_run.go`, `event.go`, and `Model()`/`saveSessionUsage` in
`agent_session.go` all use `a.primaryModel.Get()`.
`Summarize()` also uses `a.primaryModel.Get()` so compact uses the same
active tier/provider/model as the current session.
Compact summarization keeps the normal tagged-bash system prompt as the first
message and appends the compact instruction as the final user message. This
keeps the shared prompt prefix cacheable.

### CLI behavior matrix

| Command | ActiveTier | Model override |
|---|---|---|
| `lenos` / `lenos run` | Large | none |
| `lenos --small-model` / `lenos run --small-model` | Small | none |
| `lenos -m gpt-4o` | Large | Models[Large] = gpt-4o |
| `lenos -m gpt-4o --small-model` | Small | Models[Small] = gpt-4o |
| `lenos config set-model large gpt-4o` | (persistent write) | config.json |

CLI flags are ephemeral (no JSON write). Use `lenos config set-model` for
explicit persistence. Summarize uses the active tier selected for the
current session.

### Key Dependency Roles

- **`charm.land/fantasy`**: LLM provider abstraction layer. Handles protocol
  differences between Anthropic, OpenAI, Gemini, etc. Used in `internal/app`
  and `internal/agent`.
- **`charm.land/bubbletea/v2`**: TUI framework powering the interactive UI.
- **`charm.land/lipgloss/v2`**: Terminal styling.
- **`charm.land/glamour/v2`**: Markdown rendering in the terminal.
- **`charm.land/catwalk`**: Snapshot/golden-file testing for TUI components.
- **`sqlc`**: Generates Go code from SQL queries in `internal/db/sql/`.

### Key Patterns

- **Config is a Service**: accessed via `config.Service`, not global state.
- **Tools are self-documenting**: each tool has a `.go` implementation and a
  `.md` description file in `internal/agent/tools/`.
- **System prompts are Go templates**: `internal/agent/templates/*.md.tpl`
  with runtime data injected.
- **Context files**: Lenos reads AGENTS.md, LENOS.md, CLAUDE.md, GEMINI.md
  (and `.local` variants) from the working directory for project-specific
  instructions.
- **Persistence**: SQLite + sqlc. All queries live in `internal/db/sql/`,
  generated code in `internal/db/`. Migrations in `internal/db/migrations/`.
- **Pub/sub**: `internal/pubsub` for decoupled communication between agent,
  UI, and services.
- **CGO disabled**: builds with `CGO_ENABLED=0` and
  `GOEXPERIMENT=greenteagc`.

### Lenos Bash Storage Invariant

Assistant messages in the database should preserve the full tagged-bash
response the agent emitted, after any runtime auto-repair. For example, if
the model emits plain reader-facing prose and the runtime repairs it to a
bash block, store the repaired response as the assistant message. This keeps
future model history full of valid protocol examples and teaches the
protocol through its own prior turns.

Published message bodies are display data parsed from the stored assistant
emit. Do not duplicate command output onto result rows. Synthetic mixed
emits must store the raw tagged-bash response as assistant history while
executing only the cleaned bash. When changing response rendering, preserve
the existing TUI prose renderer semantics; storage role changes should not
make markdown look raw or change row alignment. See `internal/ui/AGENTS.md`
for chat render rules.

## Build/Test/Lint Commands

- **Build**: `go build .` or `go run .`
- **Test**: `task test` or `go test ./...` (run single test:
  `go test ./internal/llm/prompt -run TestGetContextFromPaths`)
- **Update Golden Files**: `go test ./... -update` (regenerates `.golden`
  files when test output changes)
  - Update specific package:
    `go test ./internal/agent -update` (in this case,
    we're updating test golden files)
- **Lint**: `task lint:fix`
- **Format**: `task fmt` (`gofumpt -w .`)
- **Modernize**: `task modernize` (runs `modernize` which makes code
  simplifications)
- **Dev**: `task dev` (runs with profiling enabled)

## Code Style Guidelines

- **Imports**: Use `goimports` formatting, group stdlib, external, internal
  packages.
- **Formatting**: Use gofumpt (stricter than gofmt), enabled in
  golangci-lint.
- **Naming**: Standard Go conventions — PascalCase for exported, camelCase
  for unexported.
- **Types**: Prefer explicit types, use type aliases for clarity (e.g.,
  `type AgentName string`).
- **Error handling**: Return errors explicitly, use `fmt.Errorf` for
  wrapping.
- **Context**: Always pass `context.Context` as first parameter for
  operations.
- **Interfaces**: Define interfaces in consuming packages, keep them small
  and focused.
- **Structs**: Use struct embedding for composition, group related fields.
- **Constants**: Use typed constants with iota for enums, group in const
  blocks.
- **Testing**: Use testify's `require` package, parallel tests with
  `t.Parallel()`, `t.SetEnv()` to set environment variables. Always use
  `t.Tempdir()` when in need of a temporary directory. This directory does
  not need to be removed.
- **System prompt tests**: Prefer stable behavior and rendering invariants
  over exact prompt prose. Check dynamic data injection, required protocol
  anchors, and forbidden legacy guidance; avoid brittle copy assertions that
  force test edits for harmless wording changes.
- **JSON tags**: Use snake_case for JSON field names.
- **File permissions**: Use octal notation (0o755, 0o644) for file
  permissions.
- **Log messages**: Log messages must start with a capital letter (e.g.,
  "Failed to save session" not "failed to save session").
  - This is enforced by `task lint:log` which runs as part of `task lint`.
- **Comments**: End comments in periods unless comments are at the end of the
  line.

## Testing with Mock Providers

When writing tests that involve provider configurations, use the mock
providers to avoid API calls:

```go
func TestYourFunction(t *testing.T) {
    // Enable mock providers for testing
    originalUseMock := config.UseMockProviders
    config.UseMockProviders = true
    defer func() {
        config.UseMockProviders = originalUseMock
        config.ResetProviders()
    }()

    // Reset providers to ensure fresh mock data
    config.ResetProviders()

    // Your test code here - providers will now return mock data
    providers := config.Providers()
    // ... test logic
}
```

## Formatting

- ALWAYS format any Go code you write.
  - First, try `gofumpt -w .`.
  - If `gofumpt` is not available, use `goimports`.
  - If `goimports` is not available, use `gofmt`.
  - You can also use `task fmt` to run `gofumpt -w .` on the entire project,
    as long as `gofumpt` is on the `PATH`.

## Comments

- Comments that live on their own lines should start with capital letters and
  end with periods. Wrap comments at 78 columns.

## Committing

- ALWAYS use semantic commits (`fix:`, `feat:`, `chore:`, `refactor:`,
  `docs:`, `sec:`, etc).
- Try to keep commits to one line, not including your attribution. Only use
  multi-line commits when additional context is truly necessary.

## Working on the TUI (UI)

Anytime you need to work on the TUI, read `internal/ui/AGENTS.md` before
starting work.
