package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSystemPromptCmd_AgentFlagDeclared(t *testing.T) {
	f := systemPromptCmd.Flags().Lookup("agent")
	require.NotNil(t, f, "--agent flag must be declared on systemPromptCmd")
}

func TestSystemPromptCmd_ContextFileFlagDeclared(t *testing.T) {
	f := systemPromptCmd.Flags().Lookup("context-file")
	require.NotNil(t, f, "--context-file flag must be declared on systemPromptCmd")
}
