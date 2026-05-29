# TASKS1: Fork mvdan Shell Parser For Lenos Bash

## Goal

Create parser support for Lenos Bash message blocks in the existing
`mvdan.cc/sh/v3/syntax` fork at `/home/neil/code/projects/tta-lab/sh`, so Lenos
can distinguish top-level message blocks from shell strings, comments,
heredocs, substitutions, and control flow.

## Status

Complete in the `sh` fork on branch `feat/message-blocks`.

- PR: https://github.com/tta-lab/sh/pull/2
- Final pushed commit reviewed here: `3e635741 fix(syntax): enforce line-start
  message blocks`
- Fork tag for Lenos consumption: `v3.13.1-lensh.2`, pointing at `3e635741`.
  Workers should reference this tag from Lenos rather than a floating branch or
  unlabelled hash.
- API exposed by the fork:
  - `syntax.ScanMsgBlocks(src []byte, baseOffset uint) ([]*MessageBlock, []byte, error)`
  - `syntax.TryParseMsgBlock(src []byte, offset uint, line, col uint) (*MessageBlock, int, error)`
  - `syntax.EscapeMessageBlock(body string) string`

Do not re-open parser implementation in Lenos workers unless PR #2 changes.
TASKS2 should consume this API.

## Dependency

Start after TASKS0 has the initial spec and fixtures.

Runtime integration in Lenos must wait until this phase is complete.

## References

- `flicknote://note/09b84178-47fe-49dc-bb40-a3ab62c2bd84`
- `flicknote://note/74688c39-dde4-4333-92d2-5330f99e0f22`
- `flicknote://note/ca4e2e09-f70d-4cfe-a9d2-c066df03a9a3`
- `flicknote://note/202ae86e-ed37-4497-85c2-299618cb6403`

## Required Behavior

Recognize top-level message blocks:

```bash
m"Done."

m#"
Body with "quotes".
"#

m(neil)##"
Body containing "# safely.
"##
```

Do not recognize message blocks inside:

- strings;
- comments;
- heredoc bodies, where `m"..."` is literal content and must not error;
- command substitutions;
- arithmetic contexts;
- subshells;
- functions;
- `if`, `case`, `for`, `while`, or similar control-flow bodies;
- ordinary command words.

Recognize message blocks only when `m` is the first non-whitespace token on a
physical line. Same-line message-block syntax after `;`, `&`, `|`, or heredoc
setup is a parser error, not a silent ignore.

## Parser Output

Provide an API that can return:

- extracted message blocks with source spans;
- optional target;
- body text;
- remaining bash source after removing message blocks;
- parse errors for unterminated or mismatched message delimiters.

Prefer source-span removal over reprinting the whole shell AST. The executed
bash should be original source minus message blocks, not formatter output.

## Implementation Notes

- Use Go module hygiene consistent with this repo.
- Document why upstream `mvdan.cc/sh/v3/syntax` is not enough and how the fork
  is tracked.
- Keep the fork delta narrow and easy to audit.
- Preserve normal bash parsing behavior outside Lenos extensions.
- Avoid broad regex parsing for final implementation; regex may be useful only
  in tests or fixture generation.

## Acceptance

- Parser tests pass in the `sh` fork: `go test ./syntax -count=1`,
  `go test ./...`, `go vet ./...`.
- Top-level line-start valid message blocks are extracted exactly.
- Same-line message-block forms produce a protocol error.
- Heredoc-body `m"..."` remains literal content.
- Source spans are stable enough for runtime removal and diagnostics.
- No Lenos agent loop behavior is changed in this phase.

## Handoff

TASKS2 should wire Lenos to `syntax.ScanMsgBlocks` and normal mvdan parsing.
If module fetching for the fork is not straightforward because the fork keeps
module path `mvdan.cc/sh/v3`, make dependency wiring the first explicit Lenos
integration step rather than hiding it in runtime work.

Preferred dependency reference for workers:

1. Use `github.com/tta-lab/sh` tag `v3.13.1-lensh.2`, which points at the
   reviewed parser commit `3e635741`.
2. Add the Lenos dependency in a way that imports remain
   `mvdan.cc/sh/v3/syntax`.
3. Expected durable shape is a non-local module replacement such as
   `replace mvdan.cc/sh/v3 => github.com/tta-lab/sh v3.13.1-lensh.2` if Go
   accepts it with this fork/module-path layout.
4. If Go module resolution cannot fetch the fork tag directly because of the
   upstream module path, use a temporary local `replace mvdan.cc/sh/v3 =>
   /home/neil/code/projects/tta-lab/sh` only to unblock local development, and
   call out the durable dependency decision in the PR. Do not silently commit an
   absolute local replace.
