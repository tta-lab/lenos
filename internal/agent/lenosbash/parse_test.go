package lenosbash

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProseOnly(t *testing.T) {
	t.Parallel()

	source := "Let me check the files."
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	assert.Empty(t, parsed.Bash)
	assert.Equal(t, source, parsed.Prose)
}

func TestParseBashOnly(t *testing.T) {
	t.Parallel()

	source := BashBlock("ls -la")
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	assert.Empty(t, parsed.Prose)
	require.Len(t, parsed.Bash, 1)
	assert.Equal(t, "ls -la\n", parsed.Bash[0])
}

func TestParseProseThenBash(t *testing.T) {
	t.Parallel()

	source := "Let me check the files.\n" + BashBlock("ls -la\ncat README.md") + "\nLooks good."
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	require.Len(t, parsed.Bash, 1)
	assert.Equal(t, "ls -la\ncat README.md\n", parsed.Bash[0])
	assert.Contains(t, parsed.Prose, "Let me check the files")
	assert.Contains(t, parsed.Prose, "Looks good")
}

func TestParseMultipleBashBlocks(t *testing.T) {
	t.Parallel()

	source := BashBlock("ls") + "\nDone.\n" + BashBlock("pwd")
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	require.Len(t, parsed.Bash, 2)
	assert.Equal(t, "ls\n", parsed.Bash[0])
	assert.Equal(t, "pwd\n", parsed.Bash[1])
	assert.Equal(t, "Done.\n", parsed.Prose)
}

func TestParseEmptyString(t *testing.T) {
	t.Parallel()

	parsed, diag := Parse("")

	require.Nil(t, diag)
	assert.Empty(t, parsed.Bash)
	assert.Empty(t, parsed.Prose)
}

func TestParseWhitespaceOnly(t *testing.T) {
	t.Parallel()

	parsed, diag := Parse("  \n  \t  ")

	require.Nil(t, diag)
	assert.Empty(t, parsed.Bash)
	assert.Empty(t, parsed.Prose)
}

func TestParseBashInsideBashIsLiteral(t *testing.T) {
	t.Parallel()

	source := BashStartTag + "\ncat <<'EOF' | src edit main.go\n===BEFORE===\n" +
		BashStartTag + "\nhello\n" + BashEndTag + "\n===AFTER===\n" +
		BashStartTag + "\nworld\n" + BashEndTag + "\nEOF\n" + BashEndTag
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	require.Len(t, parsed.Bash, 1)
	body := parsed.Bash[0]
	assert.Contains(t, body, "===BEFORE===")
	assert.Contains(t, body, BashStartTag)
	assert.Contains(t, body, BashEndTag)
	assert.Contains(t, body, "===AFTER===")
}

func TestParseExtraCloseTagIgnored(t *testing.T) {
	t.Parallel()

	source := BashBlock("ls") + "\n" + BashEndTag
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	require.Len(t, parsed.Bash, 1)
	assert.Equal(t, "ls\n", parsed.Bash[0])
	assert.Empty(t, parsed.Prose)
}

func TestParseUnclosedTagReturnsDiagnostic(t *testing.T) {
	t.Parallel()

	source := "Let me check.\n" + BashStartTag + "\nls -la\n"
	_, diag := Parse(source)

	require.NotNil(t, diag)
	assert.Equal(t, "tag_unclosed", diag.Kind)
	assert.Contains(t, diag.Message, "unclosed "+BashStartTag)
	assert.True(t, diag.Incomplete)
}

func TestParseLessThanInProseIsNotTag(t *testing.T) {
	t.Parallel()

	source := "use < for stdin redirection"
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	assert.Empty(t, parsed.Bash)
	assert.Contains(t, parsed.Prose, "use < for stdin redirection")
}

func TestParseXmlLookingTextIsPlainMarkdown(t *testing.T) {
	t.Parallel()

	source := "<note>Reviewed.</note>"
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	assert.Empty(t, parsed.Bash)
	assert.Equal(t, source, parsed.Prose)
}
