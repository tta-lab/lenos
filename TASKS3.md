# TASKS3: Add Optional Assistant Prefill

## Goal

Add config-driven assistant prefill support so providers with native prefix
completion can start a response with `m`, improving adherence to Lenos Bash
message-block syntax.

## Dependency

Start after TASKS2 runtime support exists. Prefer starting after TASKS2.1 so
prefill repair paths can use the new diagnostic renderer. It may be implemented
earlier only behind a disabled flag with no behavior change.

## References

- `flicknote://note/09b84178-47fe-49dc-bb40-a3ab62c2bd84`
- `flicknote://note/74688c39-dde4-4333-92d2-5330f99e0f22`
- `flicknote://note/ca4e2e09-f70d-4cfe-a9d2-c066df03a9a3`
- `flicknote://note/202ae86e-ed37-4497-85c2-299618cb6403`

## Behavior

Config should support enabling a prefill string. Suggested shape if it matches
the existing config style:

```json
{
  "options": {
    "assistant_prefill": "m"
  }
}
```

The prefill text is `m`, not `m"`, so the model can choose delimiter length:

- `m"..."`;
- `m#"..."#`;
- `m##"..."##`;
- `m(target)"..."`.

The prefill is only safe at the beginning of a new assistant response because
the protocol requires `m` to be the first non-whitespace token on a physical
line. Do not inject prefill in the middle of an already-started assistant text
segment.

Only send prefill through provider-native prefix APIs. Do not simulate prefill
by adding fake prior assistant text unless that provider explicitly treats it
as the next-token prefix.

## Provider Notes

- DeepSeek has a prefix-completion style API and is the first likely target.
- Providers without native prefill should ignore this option or return a clear
  unsupported path, depending on the existing provider abstraction.
- Do not implement provider behavior by guessing from memory. Check the current
  provider API docs or local provider package before wiring request fields.

## Tests

- Config loading validates the option.
- Provider request construction includes prefix only for supported providers.
- Unsupported providers do not change behavior.
- Prefill can be disabled.
- Runtime still accepts pure bash when prefill is disabled.

## Acceptance

- Prefill is opt-in.
- Default behavior is unchanged.
- The provider abstraction has a clear way to represent support or lack of
  support.
- No prompt changes are required for this phase.
