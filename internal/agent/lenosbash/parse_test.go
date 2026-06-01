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

	assert.Equal(t, "<run>", BashStartTag)
	assert.Equal(t, "</run>", BashEndTag)

	source := BashBlock("ls -la")
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	assert.Empty(t, parsed.Prose)
	require.Len(t, parsed.Bash, 1)
	assert.Equal(t, "ls -la\n", parsed.Bash[0])
	assert.Equal(t, source, parsed.Accepted)
	assert.Empty(t, parsed.DroppedPostBash)
}

func TestWrapBashSeparatesProseFromTagWithBlankLine(t *testing.T) {
	t.Parallel()

	got := WrapBash("List registered projects and available skills.", "ttal project list\nskill list")

	assert.Equal(t, "List registered projects and available skills.\n\n"+BashStartTag+"\nttal project list\nskill list\n"+BashEndTag, got)
}

func TestParseProseThenBash(t *testing.T) {
	t.Parallel()

	source := "Let me check the files.\n" + BashBlock("ls -la\ncat README.md")
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	require.Len(t, parsed.Bash, 1)
	assert.Equal(t, "ls -la\ncat README.md\n", parsed.Bash[0])
	assert.Equal(t, "Let me check the files.\n", parsed.Prose)
	assert.Equal(t, source, parsed.Accepted)
	assert.Empty(t, parsed.DroppedPostBash)
}

func TestParseDropsTextAfterFirstBashBlock(t *testing.T) {
	t.Parallel()

	source := "Before.\n" + BashBlock("ls") + "\nDone.\n"
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	require.Len(t, parsed.Bash, 1)
	assert.Equal(t, "ls\n", parsed.Bash[0])
	assert.Equal(t, "Before.\n", parsed.Prose)
	assert.Equal(t, "Before.\n"+BashBlock("ls"), parsed.Accepted)
	assert.Equal(t, "\nDone.\n", parsed.DroppedPostBash)
}

func TestParseDropsSecondBashBlock(t *testing.T) {
	t.Parallel()

	source := BashBlock("ls") + "\n" + BashBlock("pwd")
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	require.Len(t, parsed.Bash, 1)
	assert.Equal(t, "ls\n", parsed.Bash[0])
	assert.Empty(t, parsed.Prose)
	assert.Equal(t, BashBlock("ls"), parsed.Accepted)
	assert.Equal(t, "\n"+BashBlock("pwd"), parsed.DroppedPostBash)
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

func TestParseInlineBashTagsArePlainMarkdown(t *testing.T) {
	t.Parallel()

	source := "This PR switches to " + BashStartTag + "/" + BashEndTag + " tags."
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	assert.Empty(t, parsed.Bash)
	assert.Equal(t, source, parsed.Prose)
}

func TestParseIndentedBashTagsArePlainMarkdown(t *testing.T) {
	t.Parallel()

	source := "  " + BashStartTag + "\nls\n" + BashEndTag
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	assert.Empty(t, parsed.Bash)
	assert.Equal(t, "  "+BashStartTag+"\nls\n", parsed.Prose)
}

func TestParseLineMiddleEndTagInsideBashIsPlainText(t *testing.T) {
	t.Parallel()

	source := BashStartTag + "\necho " + BashEndTag + "\n" + BashEndTag
	parsed, diag := Parse(source)

	require.Nil(t, diag)
	require.Len(t, parsed.Bash, 1)
	assert.Equal(t, "echo "+BashEndTag+"\n", parsed.Bash[0])
}

func TestParseUnclosedTagReturnsPartialProseAndBash(t *testing.T) {
	t.Parallel()

	source := "Let me check.\n" + BashStartTag + "\nls -la\n"
	parsed, diag := Parse(source)

	require.NotNil(t, diag)
	assert.True(t, diag.Incomplete)
	assert.Equal(t, "Let me check.\n", parsed.Prose)
	require.Len(t, parsed.Bash, 1)
	assert.Equal(t, "ls -la\n", parsed.Bash[0])
	assert.Equal(t, source, parsed.Original)
}

func TestParseUnclosedTagWithProseAfterWhitespace(t *testing.T) {
	t.Parallel()

	source := "\n\n" + BashStartTag + "\necho hello\n"
	parsed, diag := Parse(source)

	require.NotNil(t, diag)
	assert.True(t, diag.Incomplete)
	assert.Empty(t, parsed.Prose)
	require.Len(t, parsed.Bash, 1)
	assert.Equal(t, "echo hello\n", parsed.Bash[0])
}

func TestParseUnclosedTagNoProse(t *testing.T) {
	t.Parallel()

	source := BashStartTag + "\ncat main.go\n"
	parsed, diag := Parse(source)

	require.NotNil(t, diag)
	assert.True(t, diag.Incomplete)
	assert.Empty(t, parsed.Prose)
	require.Len(t, parsed.Bash, 1)
	assert.Equal(t, "cat main.go\n", parsed.Bash[0])
}
