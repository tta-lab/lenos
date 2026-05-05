package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootCmd_ReadonlyFlagDeclared(t *testing.T) {
	f := rootCmd.Flags().Lookup("readonly")
	require.NotNil(t, f, "--readonly flag must be declared on rootCmd")
	require.Equal(t, "", f.Shorthand, "--readonly should have no shorthand")
	require.Equal(t, "false", f.DefValue, "--readonly default must be false")
}

func TestRootCmd_ReadonlyFlagParse(t *testing.T) {
	err := rootCmd.ParseFlags([]string{"--readonly"})
	require.NoError(t, err)
	v, _ := rootCmd.Flags().GetBool("readonly")
	require.True(t, v)
}
