package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReviewCmdRegistered(t *testing.T) {
	found := false
	for _, child := range rootCmd.Commands() {
		if child.Name() == "review" {
			found = true
			break
		}
	}
	require.True(t, found, "review command must be registered")
}

func TestReviewCmdHasInteractiveDefaults(t *testing.T) {
	require.Equal(t, "review", reviewCmd.Name())
	require.Contains(t, reviewCmd.Short, "Review")
	require.Empty(t, reviewCmd.Flags().Lookup("readonly"), "review must force readonly instead of exposing a toggle")
}
