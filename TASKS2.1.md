# TASKS2.1: Rust-Style Runtime Diagnostics For Model Repair

## Goal

Add a small Rust-style diagnostic renderer in Lenos for model-facing runtime
repair prompts. Use it for message-block syntax errors first, then migrate other
runtime corrections where it improves clarity.

## Dependency

Start after TASKS2 has the parser wired into Lenos enough to receive structured
diagnostics. This can land before TASKS3 and TASKS4.

## References

- Rust diagnostic guide: https://rustc-dev-guide.rust-lang.org/diagnostics.html
- annotate-snippets: https://docs.rs/annotate-snippets/
- miette: https://docs.rs/miette
- codespan-reporting overview: https://sdiehl.github.io/compiler-crates/codespan-reporting.html
- Go participle error shape: https://pkg.go.dev/github.com/alecthomas/participle/v2
- Lenos message block design: `flicknote://note/74688c39-dde4-4333-92d2-5330f99e0f22`

## Design Stance

Do not import a heavy renderer yet. Copy the diagnostic shape:

- short title;
- severity/kind;
- line/column/offset;
- source excerpt;
- caret/label;
- help text with a valid rewrite.

Keep structured data separate from rendered text so UI and model prompts can
render differently later.

## Target Types

Suggested internal shape:

```go
type Diagnostic struct {
    Kind       string // parse_error, message_block_error, banned_pattern, etc.
    Message    string
    Line       int
    Column     int
    Offset     int
    Incomplete bool
    SourceLine string
    Label      string
    Help       string
}
```

This can wrap or be built from the parser-side `SyntaxDiagnostic` from
TASKS1.1.

## Target Render Shape

Example:

```text
[runtime] invalid Lenos Bash

error: message block is not allowed inside `if`

  2 | if go test ./...; then
  3 |   m"Tests pass."
    |   ^ message blocks must be top-level
  4 | fi

help: put the message block at top level and include the next bash action

  m"Testing now."
  go test ./...
```

Another example:

```text
[runtime] invalid Lenos Bash

error: reached EOF without closing message block

  1 | m"I started
    | ^ message block starts here

help: close the message block

  m"I started."
```

## Scope

Start with:

- invalid Lenos Bash syntax;
- unterminated message block;
- mismatched raw-string hash delimiter;
- same-line message-block protocol errors, for example
  `echo ok; m"done"`;
- invalid message block placement;
- invalid `m(target)` target;
- shell parse errors reported by mvdan after message extraction;
- bash compatibility fallback failures if TASKS1.1 keeps `bash -n`.

Then consider migrating existing runtime prompts:

- banned pattern;
- tool-call wrapper;
- command-not-found;
- timeout;
- invalid raw natural language.

Do not block the first implementation on migrating every prompt.

## Best Practices To Follow

- Use plain words.
- Keep the main error short.
- Point at the source span when possible.
- Add one concrete help rewrite.
- Do not dump raw parser text when Lenos knows a better explanation.
- Avoid long lectures in the runtime prompt.
- Prefer false-negative repair over executing ambiguous text.

## Tests

Add tests for rendering:

- single-line source with caret;
- multiline source with line number context;
- non-ASCII source line where byte column may not match display width;
- diagnostic without position;
- help text with valid `m"..."` rewrite;
- invalid placement example inside `if`;
- same-line message-block example after `;` with help text telling the model to
  move `m` to its own physical line;
- heredoc-body `m"..."` is not an error case and should not be rendered as one;
- unterminated message block.

## Acceptance

- Message-block parser failures produce Rust-style repair prompts.
- Diagnostics are structured before rendering.
- The renderer is small and internal.
- Existing runtime prompts are unchanged unless deliberately migrated with tests.
- TASKS4 prompt work can assume runtime errors are clear and protocol-specific.
