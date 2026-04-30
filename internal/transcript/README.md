# internal/transcript

Bash-first session transcript renderer for lenos. Appends session events
to a human-readable `.md` file as they happen.

## What's in here

- **`format.go`** — pure render functions (stdlib-only). Imported by Phase 3
  `cmd/narrate` to write prose without pulling in db/agent dependencies.
  Covers frontmatter, user message, bash block, output block, success/failure
  trailers, runtime-event blockquote, prose, and turn-end.
- **`writer.go`** + `writer_unix.go` / `writer_windows.go` — `MdWriter`
  appends to the `.md` with per-call open → flock → write → fsync → close.
  Cross-process serialization between lenos main and `cmd/narrate` is via
  exclusive advisory `flock(2)`. `AppendStrict` returns errors honestly for
  cmd/narrate (E14); `Append` applies E8 swallow for lenos main.
- **`recorder.go`** — the `Recorder` interface (the Phase 1 ↔ Phase 2 seam),
  `NoopRecorder` (zero-state default for standalone tests), and
  `MdRecorder` (concrete impl wiring formatter + writer).
- **`testdata/`** — golden fixtures for byte-equal render assertions.

## Seam: the Recorder interface

```go
type Recorder interface {
    Open(ctx context.Context, meta Meta) error
    UserMessage(ctx context.Context, sessionID, text string) error
    AgentBashAnnounce(ctx context.Context, sessionID, bash string) (TrailerToken, error)
    BashResult(ctx context.Context, tok TrailerToken, out []byte, exitCode int, dur time.Duration) error
    BashSkipped(ctx context.Context, tok TrailerToken, sev Severity, description string) error
    RuntimeEvent(ctx context.Context, sessionID string, sev Severity, description string) error
    TurnEnd(ctx context.Context, sessionID string) error
    Close() error
}
```

**TrailerToken invariant.** `AgentBashAnnounce` returns an opaque token. The
caller MUST forward it to exactly one of `BashResult` (subprocess ran;
trailer/output rendered) or `BashSkipped` (banned/invalid pre-flight
rejected; runtime-event blockquote rendered, no output block, no trailer).

## How each plane uses this package

### Phase 1 — agent loop (`internal/agent/`)

Accept `transcript.Recorder` as a constructor field. Default to
`transcript.NoopRecorder{}` so unit tests don't need a real file. The
composition root (Phase 5 `cmd/lenos`) supplies `transcript.NewMdRecorder(path)`.
The agent loop also writes the same events to sqlite via `internal/db` — the
two destinations serve different consumers (model context vs. human render).

### Phase 3 — narrate CLI (`cmd/narrate/`)

Import `transcript` for `RenderProse`, `RenderRuntimeEvent`, etc. Use
`MdWriter.AppendStrict` to write prose to the same `.md` lenos main is
writing to. AppendStrict returns errors honestly (E14 fail-loud); the flock
contract keeps the two processes from interleaving partial writes.

### Phase 4 — TUI *(planned)*

The `.md` is append-only — tail it with `fsnotify` or polling. If the `.md`
is missing or stale, Phase 4 re-renders the file from sqlite (sqlite is the
SSOT). That re-render path lives in Phase 4, not in this package.

## Relationship to `internal/session/`

`internal/session/` holds the SQLite session + Todo CRUD service consumed by
both the agent loop and the chat UI. `internal/transcript/` is the
human-facing `.md` render artifact written by lenos main + cmd/narrate. The
two have non-overlapping responsibilities and both stay.

## Spec references

- `7015e7aa` — orientation (parent epic)
- `57a09f51` — render format spec (this package implements)
- `30666153` — error / edge-case handling (E7–E14 are this package's scope)
