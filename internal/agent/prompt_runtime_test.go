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
	assert.Contains(t, got, `narrate "message"`)
	assert.Contains(t, got, "# comment text")
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
