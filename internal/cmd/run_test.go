package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "run"}
	cmd.Flags().StringP("agent", "a", "", "")
	cmd.Flags().StringArrayP("context-file", "f", nil, "")
	cmd.Flags().Bool("readonly", false, "")
	cmd.Flags().Bool("no-sandbox", false, "")
	cmd.Flags().String("pair-with", "", "")
	cmd.Flags().String("usage-json", "", "")
	cmd.Flags().String("reasoning-effort", "", "")
	return cmd
}

func TestRunCmd_ReasoningEffortFlagDeclared(t *testing.T) {
	cmd := newRunCmd()
	f := cmd.Flags().Lookup("reasoning-effort")
	require.NotNil(t, f, "--reasoning-effort flag must be declared on runCmd")
	require.Equal(t, "", f.Shorthand, "--reasoning-effort should have no shorthand")
	require.Equal(t, "", f.DefValue, "--reasoning-effort default must be empty")
}

func TestRunCmd_AgentFlagDeclared(t *testing.T) {
	f := runCmd.Flags().Lookup("agent")
	require.NotNil(t, f, "--agent flag must be declared on runCmd")
	require.Equal(t, "a", f.Shorthand)
}

func TestRunCmd_ContextFileFlagDeclared(t *testing.T) {
	f := runCmd.Flags().Lookup("context-file")
	require.NotNil(t, f, "--context-file flag must be declared on runCmd")
	require.Equal(t, "f", f.Shorthand)
}

func TestRunCmd_AgentFlagParse(t *testing.T) {
	cmd := newRunCmd()
	err := cmd.ParseFlags([]string{"--agent", "coder", "hi"})
	require.NoError(t, err)
	v, _ := cmd.Flags().GetString("agent")
	require.Equal(t, "coder", v)
}

func TestRunCmd_ContextFileFlagParse(t *testing.T) {
	cmd := newRunCmd()
	err := cmd.ParseFlags([]string{"--context-file", "/tmp/test.md", "hi"})
	require.NoError(t, err)
	v, _ := cmd.Flags().GetStringArray("context-file")
	require.Equal(t, []string{"/tmp/test.md"}, v)
}

func TestRunCmd_ContextFileFlagRepeatable(t *testing.T) {
	cmd := newRunCmd()
	err := cmd.ParseFlags([]string{"--context-file", "/tmp/a.md", "--context-file", "/tmp/b.md", "hi"})
	require.NoError(t, err)
	v, _ := cmd.Flags().GetStringArray("context-file")
	require.Equal(t, []string{"/tmp/a.md", "/tmp/b.md"}, v)
}

func TestRunCmd_AgentFlagAfterSubcommand(t *testing.T) {
	cmd := newRunCmd()
	err := cmd.ParseFlags([]string{"--agent", "coder", "ping"})
	require.NoError(t, err, "cobra must accept --agent on runCmd")
	v, _ := cmd.Flags().GetString("agent")
	require.Equal(t, "coder", v)
}

func TestRunCmd_ReadonlyFlagDeclared(t *testing.T) {
	f := runCmd.Flags().Lookup("readonly")
	require.NotNil(t, f, "--readonly flag must be declared on runCmd")
	require.Equal(t, "", f.Shorthand, "--readonly should have no shorthand")
	require.Equal(t, "false", f.DefValue, "--readonly default must be false")
}

func TestRunCmd_NoSandboxFlagDeclared(t *testing.T) {
	f := runCmd.Flags().Lookup("no-sandbox")
	require.NotNil(t, f, "--no-sandbox flag must be declared on runCmd")
	require.Equal(t, "", f.Shorthand, "--no-sandbox should have no shorthand")
	require.Equal(t, "false", f.DefValue, "--no-sandbox default must be false")
}

func TestRunCmd_ReadonlyFlagParse(t *testing.T) {
	cmd := newRunCmd()
	err := cmd.ParseFlags([]string{"--readonly", "hi"})
	require.NoError(t, err)
	v, _ := cmd.Flags().GetBool("readonly")
	require.True(t, v)
}

func TestRunCmd_NoSandboxFlagParse(t *testing.T) {
	cmd := newRunCmd()
	err := cmd.ParseFlags([]string{"--no-sandbox", "hi"})
	require.NoError(t, err)
	v, _ := cmd.Flags().GetBool("no-sandbox")
	require.True(t, v)
}

func TestRunCmd_PairWithFlagDeclared(t *testing.T) {
	f := runCmd.Flags().Lookup("pair-with")
	require.NotNil(t, f, "--pair-with flag must be declared on runCmd")
	require.Equal(t, "", f.Shorthand, "--pair-with should have no shorthand")
	require.Equal(t, "", f.DefValue, "--pair-with default must be empty")
}

func TestRunCmd_UsageJSONFlagDeclared(t *testing.T) {
	f := runCmd.Flags().Lookup("usage-json")
	require.NotNil(t, f, "--usage-json flag must be declared on runCmd")
}

func TestRunCmd_UsageJSONFlagParse(t *testing.T) {
	cmd := newRunCmd()
	err := cmd.ParseFlags([]string{"--usage-json", "/tmp/usage.json"})
	require.NoError(t, err)
	value, err := cmd.Flags().GetString("usage-json")
	require.NoError(t, err)
	require.Equal(t, "/tmp/usage.json", value)
}

func TestRunCmd_PairWithFlagParse(t *testing.T) {
	cmd := newRunCmd()
	err := cmd.ParseFlags([]string{"--pair-with", "reviewer", "hi"})
	require.NoError(t, err)
	v, _ := cmd.Flags().GetString("pair-with")
	require.Equal(t, "reviewer", v)
}
