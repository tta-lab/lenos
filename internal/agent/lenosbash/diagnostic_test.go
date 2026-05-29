package lenosbash

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderDiagnosticSingleLineWithCaret(t *testing.T) {
	t.Parallel()

	rendered := RenderDiagnostic("echo ok; m\"Done.\"\n", Diagnostic{
		Kind:    "message_block_error",
		Message: "message block must start at the beginning of a physical line",
		Line:    1,
		Column:  10,
		Offset:  9,
	})

	assert.Contains(t, rendered, "[runtime] invalid Lenos Bash")
	assert.Contains(t, rendered, "error: message block must start at the beginning of a physical line")
	assert.Contains(t, rendered, "  1 | echo ok; m\"Done.\"")
	assert.Contains(t, rendered, "    |          ^ move `m` to its own physical line")
	assert.Contains(t, rendered, "help: put the message block on its own line")
	assert.Contains(t, rendered, "  m\"Testing now.\"")
}

func TestRenderDiagnosticMultilineWithContext(t *testing.T) {
	t.Parallel()

	source := "if true; then\n  m\"Done.\"\nfi\n"
	rendered := RenderDiagnostic(source, Diagnostic{
		Kind:    "message_block_error",
		Message: "message block is only valid at top level",
		Line:    2,
		Column:  3,
		Offset:  16,
	})

	assert.Contains(t, rendered, "  1 | if true; then")
	assert.Contains(t, rendered, "  2 |   m\"Done.\"")
	assert.Contains(t, rendered, "    |   ^ message blocks must be top-level")
	assert.Contains(t, rendered, "  3 | fi")
}

func TestRenderDiagnosticPreservesBlankSourceLines(t *testing.T) {
	t.Parallel()

	source := "if true; then\n\n  m\"Done.\"\nfi\n"
	rendered := RenderDiagnostic(source, Diagnostic{
		Kind:    "message_block_error",
		Message: "message block is only valid at top level",
		Line:    3,
		Column:  3,
		Offset:  16,
	})

	assert.Contains(t, rendered, "  2 | ")
	assert.Contains(t, rendered, "  3 |   m\"Done.\"")
	assert.Contains(t, rendered, "    |   ^ message blocks must be top-level")
	assert.Contains(t, rendered, "  4 | fi")
}

func TestRenderDiagnosticNonASCIIColumnUsesByteColumn(t *testing.T) {
	t.Parallel()

	rendered := RenderDiagnostic("éé m\"Done.\"\n", Diagnostic{
		Kind:    "message_block_error",
		Message: "message block must start at the beginning of a physical line",
		Line:    1,
		Column:  6,
		Offset:  5,
	})

	assert.Contains(t, rendered, "  1 | éé m\"Done.\"")
	assert.Contains(t, rendered, "    |    ^ move `m` to its own physical line")
}

func TestRenderDiagnosticWithoutPosition(t *testing.T) {
	t.Parallel()

	rendered := RenderDiagnostic("echo ok\n", Diagnostic{
		Kind:    "shell_parse_error",
		Message: "could not parse command",
	})

	assert.Contains(t, rendered, "error: could not parse command")
	assert.NotContains(t, rendered, " | ")
	assert.Contains(t, rendered, "help: emit valid bash or put natural language in a message block")
}

func TestRenderDiagnosticUnterminatedMessageBlock(t *testing.T) {
	t.Parallel()

	rendered := RenderDiagnostic("m\"I started\n", Diagnostic{
		Kind:       "message_block_error",
		Message:    "reached EOF without closing message block",
		Line:       1,
		Column:     1,
		Offset:     0,
		Incomplete: true,
	})

	assert.Contains(t, rendered, "error: reached EOF without closing message block")
	assert.Contains(t, rendered, "    | ^ message block starts here")
	assert.Contains(t, rendered, "help: use a matching closing delimiter")
	assert.Contains(t, rendered, "more `#`")
	assert.Contains(t, rendered, "  m#####\"")
	assert.Contains(t, rendered, "m####\"...\"####")
	assert.Contains(t, rendered, "  \"#####")
}

func TestRenderDiagnosticUnterminatedMessageBlockTeachesLongerOuterDelimiter(t *testing.T) {
	t.Parallel()

	source := "m####\"\nIt worked. The body mentions `m####\"...\"####` inside prose.\n\"####\n"
	_, diag := Parse(source)
	if assert.NotNil(t, diag) {
		rendered := RenderDiagnostic(source, *diag)
		assert.Contains(t, rendered, "help: use a matching closing delimiter")
		assert.Contains(t, rendered, "If the body contains `\"####`, wrap the outer message with more `#`")
		assert.Contains(t, rendered, "  m#####\"")
		assert.Contains(t, rendered, "  Text can mention m####\"...\"#### safely.")
		assert.Contains(t, rendered, "  \"#####")
	}
}

func TestRenderDiagnosticDoesNotRenderHeredocLiteralAsError(t *testing.T) {
	t.Parallel()

	_, diag := Parse("cat <<EOF\nm\"literal\"\nEOF\n")

	assert.Nil(t, diag)
}

func TestParseDiagnosticRenderIncludesValidRewrite(t *testing.T) {
	t.Parallel()

	source := "echo ok; m\"Done.\"\n"
	_, diag := Parse(source)
	if assert.NotNil(t, diag) {
		rendered := RenderDiagnostic(source, *diag)
		assert.Contains(t, rendered, "m\"Testing now.\"")
	}
}
