package main

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOrganonToolIncludesProjectAndGoalCLIs(t *testing.T) {
	t.Parallel()

	var organon tool
	for _, candidate := range tools {
		if candidate.Repo == "organon" {
			organon = candidate
			break
		}
	}

	require.NotEmpty(t, organon.Repo)
	require.ElementsMatch(t, []string{"src", "web", "skill", "project", "goal"}, organon.Binaries)
	require.True(t, slices.Contains(organon.Binaries, "project"))
	require.True(t, slices.Contains(organon.Binaries, "goal"))
}

func TestEinaiToolIncluded(t *testing.T) {
	t.Parallel()

	var einai tool
	for _, candidate := range tools {
		if candidate.Repo == "einai" {
			einai = candidate
			break
		}
	}

	require.NotEmpty(t, einai.Repo)
	require.Equal(t, "Einai", einai.Name)
	require.Equal(t, "ei", einai.Binary)
	require.True(t, einai.UseReleaseAPI)
}
