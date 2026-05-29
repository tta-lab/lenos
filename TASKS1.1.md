# TASKS1.1: Verify mvdan Fork Structured Error Support

## Goal

Confirm the `mvdan.cc/sh/v3/syntax` fork exposes the structured position and
parse-error data Lenos needs, and make sure TASKS1 parser APIs preserve that
information for Lenos runtime diagnostics.

This is parser-side only. Lenos runtime rendering belongs in TASKS2.1.

## Dependency

Run during or immediately after TASKS1 parser work.

## References

- Crush repo: `/home/neil/code/references/github.com/charmbracelet/crush`
- Shell parser fork: `/home/neil/code/projects/tta-lab/sh`
- Lenos message block design: `flicknote://note/74688c39-dde4-4333-92d2-5330f99e0f22`
- Epic MOC: `flicknote://note/09b84178-47fe-49dc-bb40-a3ab62c2bd84`

## Findings From Crush

Crush already parses shell source with mvdan before execution:

- `/home/neil/code/references/github.com/charmbracelet/crush/internal/shell/run.go`
  - `Run` calls `syntax.NewParser().Parse(strings.NewReader(opts.Command), "")`.
  - Parse errors are returned as `fmt.Errorf("could not parse command: %w", err)`.
- `/home/neil/code/references/github.com/charmbracelet/crush/internal/shell/shell.go`
  - `execCommon` uses the same parse-before-run pattern.
- `/home/neil/code/references/github.com/charmbracelet/crush/internal/fsext/expand.go`
  - Uses `syntax.NewParser().Document(...)` for shell-like word parsing.

Crush benefits from mvdan's parser, but it mostly wraps parser errors as text.
It does not appear to extract structured line/column/offset fields for model
repair prompts.

## Findings From mvdan Fork

The upstream/fork parser has structured parser errors:

```go
type ParseError struct {
    Filename string
    Pos      Pos
    Text     string

    Incomplete bool
}
```

`ParseError.Error()` formats errors as:

```text
<line>:<column>: <text>
```

or with filename:

```text
<filename>:<line>:<column>: <text>
```

`syntax.Pos` exposes:

- `Offset() uint`
- `Line() uint`
- `Col() uint`
- `String() string`
- `IsValid() bool`
- `IsRecovered() bool`

Column counts bytes, not runes. Lenos rendering should account for this if it
shows carets under non-ASCII source text.

The fork also has:

- `syntax.IsIncomplete(err)` for EOF/incomplete forms such as unclosed quotes,
  `${...`, or unmatched constructs.
- `syntax.LangError`, with structured position and language feature details.
- `syntax.RecoverErrors(maximum)`, mainly useful for interactive completion or
  best-effort AST recovery. It should not be the default for strict runtime
  validation.
- `syntax.KeepComments(true)` if comments need to be preserved in the parsed
  tree.

The Lenos fork now also has message-block errors:

```go
type MessageBlockError struct {
    Pos     Pos
    Message string
}

func (e MessageBlockError) Incomplete() bool
```

`MessageBlockError` is returned for unterminated blocks, invalid targets,
mismatched delimiters, and same-line message-block protocol errors such as
`echo ok; m"done"`. A message-block-looking string inside a heredoc body is not
an error because it is literal content.

## Required Lenos Adapter Data

TASKS2 should wrap the fork API in a small Lenos adapter that preserves enough
information for Rust-style model-facing diagnostics later:

```go
type SyntaxDiagnostic struct {
    Message    string
    Line       int
    Column     int
    Offset     int
    Incomplete bool
    Filename   string
}

type ParsedLenosBash struct {
    Bash     string
    Messages []MessageBlock
}

func ParseLenosBash(source string) (ParsedLenosBash, *SyntaxDiagnostic)
```

Names can change, but the data must survive the parser boundary.

The Lenos adapter should convert `syntax.ParseError`, `syntax.LangError`, and
Lenos message-block parse errors into this structured diagnostic shape. Unknown
parser errors can fall back to `Message` only.

Concrete fork API to consume:

```go
blocks, clean, err := syntax.ScanMsgBlocks([]byte(source), 0)
```

Then run normal mvdan parsing on `clean` to validate the remaining bash.
`clean` preserves byte positions by replacing extracted message spans with
spaces while preserving newlines, so diagnostics can still point into the
original assistant source.

## Acceptance

- TASKS2 adapter exposes message, line, column, offset, incomplete flag, and
  filename where available.
- Lenos-specific message block errors use the same diagnostic shape.
- The parser can report invalid delimiter and invalid placement with useful
  positions.
- Same-line message-block syntax is surfaced as a protocol diagnostic, not
  passed through to bash as a command-not-found.
- No Lenos runtime renderer is implemented in this phase.

## Handoff

TASKS2 wires the parser into Lenos. TASKS2.1 renders these structured
diagnostics into model-facing repair prompts.
