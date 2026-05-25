package agent

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tta-lab/lenos/internal/agent/prompt"
)

func TestBuildRuntimeContextCommandsLabelsInstructionScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	userFile := filepath.Join(home, ".claude", "CLAUDE.md")
	projectFile := filepath.Join(t.TempDir(), "AGENTS.md")
	commands := buildRuntimeContextCommands(prompt.RuntimeContext{
		ContextFiles: []prompt.ContextFile{
			{Path: userFile},
			{Path: projectFile},
		},
	})

	require.Len(t, commands, 4)
	require.Equal(t, "# check registered projects\nttal project list", commands[0].Command)
	require.True(t, commands[0].Optional)
	require.Equal(t, "# list available skills\nskill list", commands[1].Command)
	require.True(t, commands[1].Optional)
	require.Equal(t, "# read user-scope instructions\ncat "+shellQuote(userFile), commands[2].Command)
	require.Equal(t, "# read project-scope instructions\ncat "+shellQuote(projectFile), commands[3].Command)
}
