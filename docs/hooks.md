# Hooks

Lenos can fire a configurable shell command after each model step. Use this
for status reporting, observability, or custom integrations.

## Configuration

In your lenos config:

```json
{
  "hooks": {
    "post_step": "ttal status update"
  }
}
```

When `post_step` is set, lenos runs `sh -c "<post_step>"` after each step in
the agent loop, with a JSON envelope on stdin. The subprocess inherits the
lenos process env — consumers read identity from env vars (e.g.
`TTAL_AGENT_NAME`).

## Envelope schema

Stdin is a single JSON object:

```json
{
  "version": 1,
  "event": "post_step",
  "step_index": 12,
  "session_id": "01HZ...",
  "model_id": "claude-sonnet-4-5",
  "context_window": 200000,
  "input_tokens": 14523,
  "output_tokens": 421,
  "total_tokens": 14944,
  "reasoning_tokens": 87,
  "cache_creation_tokens": 0,
  "cache_read_tokens": 8200,
  "context_used_pct": 7.47,
  "context_remaining_pct": 92.53,
  "timestamp": "2026-05-05T08:08:00Z"
}
```

`agent_name` is deliberately absent — consumers read it from env. `version` and
`event` are discriminators; future events (e.g. `pre_step`) will share the
envelope shape. See `internal/hooks/schema/post_step.v1.json` for the formal
JSON Schema definition.

## Contract

The `[hooks].post_step` interface is a stable contract. Lenos commits to:

1. **Schema versioning.** The `version` field starts at 1. Breaking changes
   bump the version. The `event` discriminator stays stable across versions.
   Consumers should reject envelopes with unknown `version` values.
2. **Env inheritance.** The hook subprocess inherits the lenos process env
   unchanged. Consumers may rely on env vars set by their spawn parent (e.g.
   ttal's `TTAL_AGENT_NAME`, `TTAL_JOB_ID`, `TTAL_RUNTIME` from
   `launchcmd.BuildEnvParts`). Lenos will not introduce `LENOS_*` env vars
   without a major version bump.
3. **Failure model.** Hooks are fire-and-forget with a 5-second per-invocation
   timeout. Errors are logged at WARN; the agent loop never aborts on hook
   failure. There is no retry, no circuit breaker, and no ordering guarantee
   between hook completion and the next agent step. Consumers must be cheap
   and idempotent.

## Failure semantics

- 5-second timeout per invocation.
- Errors are logged at WARN; the agent loop never aborts on hook failure.
- No retry, no circuit breaker. Consumers should fail fast and be cheap.
- When `post_step` is empty or `hooks` is absent, no hook is invoked.

The hook fires only after a model stream completes successfully. If a step
fails (network error, auth error, rate-limit, provider error), `runLoop`
returns immediately and `post_step` is NOT invoked for that step. Consumers
may observe gaps in `step_index` sequence — these correspond to failed steps.
Consumers that need to detect step failures should consult the lenos transcript
or session message log, not the hook stream.
