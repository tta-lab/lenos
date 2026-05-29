# TASKS0: Message Block Spec And Test Corpus

## Goal

Prepare the executable spec and test corpus for Lenos Bash message blocks before
parser/runtime changes begin.

## Context

Design notes:

- `flicknote://note/09b84178-47fe-49dc-bb40-a3ab62c2bd84`
- `flicknote://note/74688c39-dde4-4333-92d2-5330f99e0f22`
- `flicknote://note/ca4e2e09-f70d-4cfe-a9d2-c066df03a9a3`
- `flicknote://note/202ae86e-ed37-4497-85c2-299618cb6403`

Protocol summary:

- Lenos Bash is bash plus `m` Rust-style raw string message blocks.
- Message blocks are valid natural-language output. Raw top-level prose is not.
- `m` must be the first non-whitespace token on its own physical line.
- Message blocks are removed before bash execution.
- One assistant emit still maps to one bash subprocess.
- If bash remains after message removal, message blocks do not end the loop.
- If only message blocks remain, `m` publishes natural language and ends.
- Same-line message forms such as `echo ok; m"done"` are protocol errors.
- `m"..."` inside a heredoc body is literal heredoc content, not a message
  block and not an error.

## Tasks

1. Write a concise protocol spec in `docs/agent-protocol/message-blocks.md`.
2. Define valid syntax examples:
   - `m"Done."`
   - multiline `m"..."`.
   - `m#"body with "quotes""#`.
   - `m##"body with "# delimiter candidate"##`.
   - `m(neil)"addressed message"`.
   - mixed bash plus message blocks.
3. Define invalid or non-protocol examples:
   - raw prose before bash.
   - fenced bash.
   - same-line message forms after `;`, `&`, `|`, or heredoc setup.
   - message block inside `if`, loop, function, command substitution, heredoc,
     string, comment, or ordinary command word.
   - unterminated message block.
   - mismatched hash count.
   - invalid target characters.
4. Create table-driven test fixtures under a new focused location, for example
   `internal/agent/messageblock/testdata/` or another repo-consistent path.
5. Include expected outputs for each fixture:
   - extracted message events.
   - remaining bash source.
   - whether bash remains.
   - expected lifecycle class for message-only responses.
   - expected parse error, if any.

## Acceptance

- A reviewer can implement parser/runtime work from the spec without guessing.
- Test cases cover zero-hash and multi-hash delimiters.
- Test cases cover top-level-only behavior.
- Test cases cover line-start-only behavior:
  - valid: `m"done"` and indented top-level `m"done"`;
  - invalid: `echo ok; m"done"`, `cmd & m"done"`, `cmd | m"done"`;
  - literal heredoc content: `cat <<EOF\nm"literal"\nEOF`.
- The spec should not define legacy convenience variants or custom diagnostics
  for old continue concepts. Message-only `m` ends; mixed bash plus `m`
  continues through normal bash result flow.
- No runtime behavior is changed in this phase unless needed to add inert test
  fixture types.

## Handoff

TASKS1 depends on this corpus. Do not start Lenos runtime integration until
TASKS1 has parser support working against these fixtures.
