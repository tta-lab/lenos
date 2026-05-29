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
	assert.Contains(t, got, `m"`)
	assert.NotContains(t, got, "narrate")
	assert.Contains(t, got, "# ...")
}

func TestRePromptInvalidBash(t *testing.T) {
	t.Parallel()
	got := rePromptInvalidBash("syntax error near token `then'")
	assert.True(t, strings.HasPrefix(got, "[runtime] "))
	assert.Contains(t, got, "not valid bash")
	assert.Contains(t, got, "bash -n said:")
	assert.Contains(t, got, "syntax error near token `then'")
	assert.Contains(t, got, "neither bash nor a valid")
	assert.Contains(t, got, `m"`)
	assert.NotContains(t, got, "narrate")
	assert.Contains(t, got, "exit")
}

func TestRePromptBlockedPattern(t *testing.T) {
	t.Parallel()
	got := rePromptBlockedPattern()
	assert.True(t, strings.HasPrefix(got, "[runtime] "))
	assert.Contains(t, got, "sed -i / perl -i is not allowed")
	assert.Contains(t, got, "src edit")
}

func TestRePromptToolCall_NoLiteralPatterns(t *testing.T) {
	t.Parallel()
	got := rePromptToolCall()
	assert.True(t, strings.HasPrefix(got, alertPrefix+" "))
	assert.Contains(t, got, "There is NO tool/function calling API")
	assert.Contains(t, got, "emit plain bash")
	assert.Contains(t, got, `m"`)
	assert.NotContains(t, got, "narrate")
	forbidden := []string{"<tool_call>", "</tool_call>", "<function_call>", "[tool_call]", "<invoke"}
	for _, s := range forbidden {
		assert.NotContains(t, got, s, "literal wrong-shape pattern leaked into rePromptToolCall body")
	}
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
	assert.Contains(t, got, `m"`)
	assert.NotContains(t, got, "narrate")
	assert.Contains(t, got, "exit")
	assert.NotContains(t, got, "```")
	assert.Contains(t, got, "real binary you expected")
	assert.Contains(t, got, "English sentence")
	assert.Contains(t, got, "markdown fence")
}

func TestRePromptCmdNotFound_EmptyInput(t *testing.T) {
	t.Parallel()
	got := rePromptCmdNotFound("")
	assert.True(t, strings.HasPrefix(got, alertPrefix+" "))
	assert.Contains(t, got, "command not found")
	assert.Contains(t, got, `m"`)
	assert.NotContains(t, got, "narrate")
	assert.Contains(t, got, "exit")
}

func TestRePromptCmdNotFound_SpecialChars(t *testing.T) {
	t.Parallel()
	got := rePromptCmdNotFound("( ")
	assert.Contains(t, got, "`( `")
	assert.Contains(t, got, "command -v (")
}
