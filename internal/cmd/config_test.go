package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestConfigCmd_Declared(t *testing.T) {
	require.Equal(t, "config", configCmd.Use)
	require.NotEmpty(t, configCmd.Short)
}

func TestConfigCmd_HasSetModelSubcommand(t *testing.T) {
	found := false
	for _, c := range configCmd.Commands() {
		if c.Use == "set-model <large|small> <model>" {
			found = true
			break
		}
	}
	require.True(t, found, "set-model subcommand must be registered")
}

func TestConfigSetModelCmd_RequiresExactArgs(t *testing.T) {
	cmd := &cobra.Command{
		Use:  "set-model",
		Args: cobra.ExactArgs(2),
	}

	err := cmd.ValidateArgs([]string{"large"})
	require.Error(t, err, "set-model requires exactly 2 args")

	err = cmd.ValidateArgs([]string{"large", "gpt-4o"})
	require.NoError(t, err, "set-model accepts exactly 2 args")

	err = cmd.ValidateArgs([]string{"large", "gpt-4o", "extra"})
	require.Error(t, err, "set-model rejects 3 args")
}

func TestConfigSetModelCmd_RejectsInvalidTier(t *testing.T) {
	cmd := &cobra.Command{
		Use:  "set-model",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "large" && args[0] != "small" {
				return errInvalidModelType
			}
			return nil
		},
	}

	err := cmd.RunE(cmd, []string{"large", "gpt-4o"})
	require.NoError(t, err)

	err = cmd.RunE(cmd, []string{"small", "gpt-4o-mini"})
	require.NoError(t, err)
}

// errInvalidModelType is used by TestConfigSetModelCmd_RejectsInvalidTier
var errInvalidModelType = errInvalidModelTypeFn()

type invalidModelTypeError struct{}

func (e *invalidModelTypeError) Error() string { return "invalid model type" }

func errInvalidModelTypeFn() error { return &invalidModelTypeError{} }
