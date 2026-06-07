# Journal Feature Code Map

The session journal gives agents durable working memory and handoff records
across context window compactions. This file maps all the code and templates
that make it work.

## Templates (agent-facing Markdown)

| File | Role |
|---|---|
| `internal/agent/templates/journal.md` | Empty journal template agents fill out. Sections: Task, Preflight, Context, Environment, Plan, Progress, Verification, Next. |
| `internal/agent/templates/coder.md` | Coder system prompt. Session Journal section (§201+) describes the workflow: initial fill, periodic self-check, handoff on compact/timeout. Preflight paragraph references `ei ask` sync helper. |
| `internal/agent/templates/coder_context.md` | Startup synthetic response. Runs `cat $LENOS_JOURNAL` so the agent sees its journal on new context windows. |

## Go implementation

| File | Role |
|---|---|
| `internal/agent/journal.go` | Core functions: `JournalDir`, `JournalPath`, `CreateJournal` (writes template). Hint generators: `journalFillHint`, `compactHandoffHint`, `autoCompactHint`. |
| `internal/agent/coordinator.go` | Creates journal via `CreateJournal` for native coder sessions, sets `LENOS_JOURNAL` env var, passes `JournalPath` in `SessionAgentCall`. |
| `internal/agent/agent.go` | `SessionAgentCall.JournalPath` field. `SessionAgent.CompactSession()` interface for journal handoff. |
| `internal/agent/agent_run.go` | Injects journal fill hint on first turn when journal exists. Injects auto-compact hint at 80% context window usage. |
| `internal/cmd/root.go` | `formatJournalHint` displays journal path in CLI status output. |
| `internal/workspace/workspace.go` | `AgentCompactSession` interface for UI/dialog to trigger journal handoff via compact (§56). |

## UI

| File | Role |
|---|---|
| `internal/ui/dialog/actions.go` | `ActionOpenJournal` action type (§55). |
| `internal/ui/dialog/commands.go` | "Open Journal" command in compact menu, `journalExists` helper (§408-460). |
| `internal/ui/model/ui.go` | Handles `ActionOpenJournal` — opens journal file in editor (§1114-1124). |

## Tests

| File | Role |
|---|---|
| `internal/agent/agent_run_test.go` | Tests journal hint injection on first turn (§818-843). |
| `internal/agent/coordinator_test.go` | Tests journal context command in coder startup (§473-501). |
| `internal/agent/system_prompt_test.go` | Tests journal references in system prompt rendering. |

## Flow

1. Session starts → `coordinator.StartAgent` calls `CreateJournal` (template on disk)
2. `LENOS_JOURNAL` env var exported to subprocess runner
3. First turn → `agent_run.go` injects task-detection runtime hint
4. Agent reads journal via `coder_context.md` → fills sections through Plan
5. During work → periodic self-check hints at token intervals
6. Compact/timeout → handoff hint asks agent to update journal
7. New context window → `coder_context.md` runs `cat $LENOS_JOURNAL` again
