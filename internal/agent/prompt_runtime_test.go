package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRePromptEmpty(t *testing.T) {
	t.Parallel()
	got := rePromptEmpty()
	assert.True(t, strings.HasPrefix(got, "[runtime] "), "must start with [runtime] tag")
	assert.Contains(t, got, "your last response was empty")
	assert.Contains(t, got, `"exit"`)
	assert.Contains(t, got, "narrate")
	assert.Contains(t, got, "# ...")
}

func TestRePromptInvalidBash(t *testing.T) {
	t.Parallel()
	got := rePromptInvalidBash("syntax error near token `then'")
	assert.True(t, strings.HasPrefix(got, "[runtime] "))
	assert.Contains(t, got, "not valid bash")
	assert.Contains(t, got, "bash -n said:")
	assert.Contains(t, got, "syntax error near token `then'")
	assert.Contains(t, got, "natural-language prose")
	assert.Contains(t, got, "narrate <<'EOF'")
	assert.Contains(t, got, "exit")
}

func TestRePromptBlockedPattern(t *testing.T) {
	t.Parallel()
	got := rePromptBlockedPattern()
	assert.True(t, strings.HasPrefix(got, "[runtime] "))
	assert.Contains(t, got, "sed -i / perl -i is not allowed")
	assert.Contains(t, got, "src edit")
}

func TestRePromptTimeout(t *testing.T) {
	t.Parallel()
	got := rePromptTimeout(120)
	assert.True(t, strings.HasPrefix(got, "[runtime] "))
	assert.Contains(t, got, "exceeded the per-call timeout (120s)")
	assert.Contains(t, got, "timeout 30m")
}

func TestRePromptCmdNotFound_Format(t *testing.T) {
	t.Parallel()
	got := rePromptCmdNotFound("lorem")
	assert.True(t, strings.HasPrefix(got, alertPrefix+" "), "must start with [ALERT from runtime] tag")
	assert.Contains(t, got, "command not found")
	assert.Contains(t, got, "`lorem`", "first word must appear in backticks")
	assert.Contains(t, got, "command -v lorem")
	assert.Contains(t, got, "# ", "must offer bash comment for one-line inline annotation")
	assert.Contains(t, got, "comment")
	assert.Contains(t, got, "narrate <<'EOF'")
	assert.Contains(t, got, "exit")
	assert.Contains(t, got, "```bash")
	assert.Contains(t, got, "```")
	assert.Contains(t, got, "real binary you expected")
	assert.Contains(t, got, "English sentence")
	assert.Contains(t, got, "markdown fence")
}

func TestRePromptCmdNotFound_EmptyInput(t *testing.T) {
	t.Parallel()
	got := rePromptCmdNotFound("")
	assert.True(t, strings.HasPrefix(got, alertPrefix+" "))
	assert.Contains(t, got, "command not found")
	assert.Contains(t, got, "narrate")
	assert.Contains(t, got, "exit")
}

func TestRePromptCmdNotFound_SpecialChars(t *testing.T) {
	t.Parallel()
	got := rePromptCmdNotFound("( ")
	assert.Contains(t, got, "`( `")
	assert.Contains(t, got, "command -v (")
}

func TestRePromptProsePrefix_Format(t *testing.T) {
	t.Parallel()
	got := rePromptProsePrefix("Read", "Read the README first")
	assert.True(t, strings.HasPrefix(got, alertPrefix+" "), "must start with alert prefix")
	assert.Contains(t, got, "Read the README first", "must quote the offending line verbatim")
	assert.Contains(t, got, "# Read the README first", "must show comment-form conversion using the actual line")
	assert.Contains(t, got, "narrate <<'EOF'", "must show narrate-form conversion")
	assert.Contains(t, got, "command -v Read", "must offer command -v probe for cap-named binary case")
	assert.Contains(t, got, "DID NOT execute", "must signal that bash was bypassed")
}
