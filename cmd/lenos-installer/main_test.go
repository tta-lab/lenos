package main

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOrganonToolIncludesProjectCLI(t *testing.T) {
	t.Parallel()

	var organon tool
	for _, candidate := range tools {
		if candidate.Repo == "organon" {
			organon = candidate
			break
		}
	}

	require.NotEmpty(t, organon.Repo)
	require.ElementsMatch(t, []string{"src", "web", "skill", "project"}, organon.Binaries)
	require.True(t, slices.Contains(organon.Binaries, "project"))
}
