package lenosbash

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMessageOnly(t *testing.T) {
	t.Parallel()

	parsed, diag := Parse("m\"Done.\"\n")

	require.Nil(t, diag)
	assert.False(t, parsed.HasBash)
	assert.Empty(t, parsed.Bash)
	require.Len(t, parsed.Messages, 1)
	assert.Equal(t, "Done.", parsed.Messages[0].Body)
	assert.Empty(t, parsed.Messages[0].Target)
	assert.Equal(t, 1, parsed.Messages[0].Line)
	assert.Equal(t, 1, parsed.Messages[0].Column)
	assert.Equal(t, 0, parsed.Messages[0].Offset)
}

func TestParseMixedMessageAndBash(t *testing.T) {
	t.Parallel()

	parsed, diag := Parse("m\"Inspecting.\"\nrg \"needle\" .\n")

	require.Nil(t, diag)
	assert.True(t, parsed.HasBash)
	assert.Equal(t, "rg \"needle\" .\n", parsed.Bash)
	require.Len(t, parsed.Messages, 1)
	assert.Equal(t, "Inspecting.", parsed.Messages[0].Body)
}

func TestParseMessageBodyMayContainMessageBlockText(t *testing.T) {
	t.Parallel()

	source := "m####\"\nText before.\nm\"Done.\"\nText after.\n\"####\n"
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	assert.False(t, parsed.HasBash)
	require.Len(t, parsed.Messages, 1)
	assert.Contains(t, parsed.Messages[0].Body, "m\"Done.\"")
}

func TestParseMixedMessageBodyMayContainIndentedHashDelimitedExample(t *testing.T) {
	t.Parallel()

	source := "echo \"Let me demonstrate nesting.\"\nm####\"\nExample:\n\n    m###\"\n    Inner message body.\n    \"###\n\"####\n"
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	assert.True(t, parsed.HasBash)
	assert.Equal(t, "echo \"Let me demonstrate nesting.\"\n", parsed.Bash)
	require.Len(t, parsed.Messages, 1)
	assert.Contains(t, parsed.Messages[0].Body, "    m###\"")
}

func TestParseAddressedMessage(t *testing.T) {
	t.Parallel()

	parsed, diag := Parse("m(neil)#\"Please review \"message blocks\".\"#\n")

	require.Nil(t, diag)
	require.Len(t, parsed.Messages, 1)
	assert.Equal(t, "neil", parsed.Messages[0].Target)
	assert.Equal(t, "Please review \"message blocks\".", parsed.Messages[0].Body)
}

func TestParseHeredocLiteralMessageLookingText(t *testing.T) {
	t.Parallel()

	source := "cat <<EOF\nm\"literal\"\nEOF\n"
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	assert.True(t, parsed.HasBash)
	assert.Equal(t, source, parsed.Bash)
	assert.Empty(t, parsed.Messages)
}

func TestParseSameLineMessageReturnsDiagnostic(t *testing.T) {
	t.Parallel()

	_, diag := Parse("echo ok; m\"Done.\"\n")

	require.NotNil(t, diag)
	assert.Equal(t, "message_block_error", diag.Kind)
	assert.Contains(t, diag.Message, "message block must start")
	assert.Equal(t, 1, diag.Line)
	assert.Equal(t, 10, diag.Column)
	assert.Equal(t, 9, diag.Offset)
}

func TestParseNestedMessageReturnsDiagnostic(t *testing.T) {
	t.Parallel()

	_, diag := Parse("if true; then\n  m\"Done.\"\nfi\n")

	require.NotNil(t, diag)
	assert.Equal(t, "message_block_error", diag.Kind)
	assert.Contains(t, diag.Message, "top level")
	assert.Equal(t, 2, diag.Line)
	assert.Equal(t, 3, diag.Column)
	assert.Equal(t, 16, diag.Offset)
}

func TestParseInvalidTargetReturnsDiagnostic(t *testing.T) {
	t.Parallel()

	_, diag := Parse("m(bad target)\"Done.\"\n")

	require.NotNil(t, diag)
	assert.Equal(t, "message_block_error", diag.Kind)
	assert.Contains(t, diag.Message, "invalid target character")
	assert.Equal(t, 1, diag.Line)
	assert.Equal(t, 6, diag.Column)
	assert.Equal(t, 5, diag.Offset)
}

func TestParseInvalidCleanBashReturnsDiagnostic(t *testing.T) {
	t.Parallel()

	_, diag := Parse("m\"Starting.\"\nif true then\n")

	require.NotNil(t, diag)
	assert.Equal(t, "shell_parse_error", diag.Kind)
	assert.Contains(t, diag.Message, "then")
	assert.Equal(t, 2, diag.Line)
	assert.True(t, diag.Incomplete)
}
