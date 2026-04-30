package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildBaseSystemPrompt_BashFirstInvariants(t *testing.T) {
	t.Parallel()

	got, err := buildBaseSystemPrompt(promptData{
		WorkingDir: "/repo",
		Platform:   "darwin",
		Date:       "2026-04-29",
	})
	require.NoError(t, err)

	// Environment is rendered.
	assert.Contains(t, got, "Working directory: /repo")
	assert.Contains(t, got, "Platform: darwin")
	assert.Contains(t, got, "Date: 2026-04-29")

	// Bash-first protocol is described.
	assert.Contains(t, got, "raw bash")
	assert.Contains(t, got, "exit")
	assert.Contains(t, got, "narrate")
	assert.Contains(t, got, `narrate "message"`)

	// MUST NOT mention the legacy <cmd> markup — that's the whole point.
	assert.False(t, strings.Contains(got, "<cmd>"),
		"base prompt must not reference legacy <cmd> markup")
	assert.False(t, strings.Contains(got, "</cmd>"),
		"base prompt must not reference legacy </cmd> markup")

	// MUST NOT mention the legacy log CLI — narrate replaced it.
	assert.False(t, strings.Contains(got, "log info"),
		"base prompt must not reference legacy log info CLI")
	assert.False(t, strings.Contains(got, "log warn"),
		"base prompt must not reference legacy log warn CLI")
	assert.False(t, strings.Contains(got, "log error"),
		"base prompt must not reference legacy log error CLI")

	// narrate is single-mode — no severity variants.
	assert.False(t, strings.Contains(got, "narrate info"),
		"narrate is single-mode; no severity variants")
	assert.False(t, strings.Contains(got, "narrate warn"),
		"narrate is single-mode; no severity variants")
	assert.False(t, strings.Contains(got, "narrate error"),
		"narrate is single-mode; no severity variants")
}

func TestBuildBaseSystemPrompt_EmitsCommandSection(t *testing.T) {
	t.Parallel()

	got, err := buildBaseSystemPrompt(promptData{
		WorkingDir: "/repo",
		Platform:   "linux",
		Date:       "2026-04-29",
		Commands: []CommandDoc{
			{Name: "src", Summary: "symbol-aware source reader", Help: "src <file> --tree"},
			{Name: "web", Summary: "web search and fetch", Help: "web search <query>"},
		},
	})
	require.NoError(t, err)

	assert.Contains(t, got, "# Available Commands")
	assert.Contains(t, got, "## src")
	assert.Contains(t, got, "symbol-aware source reader")
	assert.Contains(t, got, "src <file> --tree")
	assert.Contains(t, got, "## web")
	assert.Contains(t, got, "web search <query>")
}

func TestBuildBaseSystemPrompt_NoCommandSectionWhenEmpty(t *testing.T) {
	t.Parallel()

	got, err := buildBaseSystemPrompt(promptData{
		WorkingDir: "/repo",
		Platform:   "linux",
		Date:       "2026-04-29",
	})
	require.NoError(t, err)
	assert.False(t, strings.Contains(got, "# Available Commands"),
		"empty Commands slice should suppress the heading")
}
