package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tta-lab/lenos/internal/agent/lenosbash"
)

func TestRePromptEmpty(t *testing.T) {
	t.Parallel()

	got := rePromptEmpty()

	assert.True(t, strings.HasPrefix(got, lenosbash.RuntimeTag+" "))
	assert.Contains(t, got, "your last response was empty")
	assert.Contains(t, got, "exit")
	assert.Contains(t, got, "bash block")
	assert.NotContains(t, got, "narrate")
}

func TestRePromptInvalidBash(t *testing.T) {
	t.Parallel()

	got := rePromptInvalidBash("syntax error near token `then'")

	assert.True(t, strings.HasPrefix(got, lenosbash.RuntimeTag+"\n"))
	assert.Contains(t, got, "not valid bash")
	assert.Contains(t, got, "bash -n said:")
	assert.Contains(t, got, "syntax error near token `then'")
	assert.NotContains(t, got, "narrate")
}

func TestRePromptBlockedPattern(t *testing.T) {
	t.Parallel()

	got := rePromptBlockedPattern()

	assert.True(t, strings.HasPrefix(got, lenosbash.RuntimeTag+"\n"))
	assert.Contains(t, got, "sed -i / perl -i is not allowed")
	assert.Contains(t, got, "src edit")
}

func TestRePromptToolCallNoLiteralPatterns(t *testing.T) {
	t.Parallel()

	got := rePromptToolCall()

	assert.True(t, strings.HasPrefix(got, alertPrefix+" "))
	assert.Contains(t, got, "There is NO tool/function calling API")
	assert.Contains(t, got, "emit a bash block")
	assert.Contains(t, got, "Markdown prose")
	assert.NotContains(t, got, "narrate")
	forbidden := []string{"<tool_call>", "</tool_call>", "<function_call>", "[tool_call]", "<invoke"}
	for _, s := range forbidden {
		assert.NotContains(t, got, s, "literal wrong-shape pattern leaked into rePromptToolCall body")
	}
}

func TestRePromptTimeout(t *testing.T) {
	t.Parallel()

	got := rePromptTimeout(120)

	assert.True(t, strings.HasPrefix(got, lenosbash.RuntimeTag+"\n"))
	assert.Contains(t, got, "exceeded the per-call timeout (120s)")
	assert.Contains(t, got, "timeout 30m")
}

func TestRePromptCmdNotFoundFormat(t *testing.T) {
	t.Parallel()

	got := rePromptCmdNotFound("lorem")

	assert.True(t, strings.HasPrefix(got, alertPrefix+" "))
	assert.Contains(t, got, "command not found")
	assert.Contains(t, got, "`lorem`")
	assert.Contains(t, got, "command -v lorem")
	assert.Contains(t, got, "Markdown prose")
	assert.NotContains(t, got, "narrate")
	assert.NotContains(t, got, "```")
	assert.Contains(t, got, "real binary you expected")
}

func TestRePromptCmdNotFoundEmptyInput(t *testing.T) {
	t.Parallel()

	got := rePromptCmdNotFound("")

	assert.True(t, strings.HasPrefix(got, alertPrefix+" "))
	assert.Contains(t, got, "command not found")
	assert.NotContains(t, got, "narrate")
}

func TestRePromptCmdNotFoundSpecialChars(t *testing.T) {
	t.Parallel()

	got := rePromptCmdNotFound("( ")

	assert.Contains(t, got, "`( `")
	assert.Contains(t, got, "command -v (")
}
