package skills

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTTALSkills(t *testing.T) {
	t.Parallel()

	t.Run("valid JSON", func(t *testing.T) {
		t.Parallel()
		input := []byte(`[{"name":"sp-planning","flicknote_id":"abc123","category":"methodology","description":"Planning skill"}]`)
		skills := parseTTALSkills(input)
		require.Len(t, skills, 1)
		require.Equal(t, "sp-planning", skills[0].Name)
		require.Equal(t, "Planning skill", skills[0].Description)
		require.Equal(t, "ttal://skills/sp-planning", skills[0].SkillFilePath)
		require.Equal(t, "ttal://skills/sp-planning", skills[0].Path)
	})

	t.Run("empty array", func(t *testing.T) {
		t.Parallel()
		skills := parseTTALSkills([]byte(`[]`))
		require.Empty(t, skills)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		t.Parallel()
		skills := parseTTALSkills([]byte(`not json`))
		require.Nil(t, skills)
	})

	t.Run("multiple skills", func(t *testing.T) {
		t.Parallel()
		input := []byte(`[{"name":"a","flicknote_id":"1","category":"tool","description":"A"},{"name":"b","flicknote_id":"2","category":"command","description":"B"}]`)
		skills := parseTTALSkills(input)
		require.Len(t, skills, 2)
		require.Equal(t, "a", skills[0].Name)
		require.Equal(t, "b", skills[1].Name)
	})
}

func TestDiscoverTTAL_NoTTAL(t *testing.T) {
	// When ttal is not in PATH, DiscoverTTAL should return nil gracefully.
	t.Setenv("PATH", t.TempDir())
	skills := DiscoverTTAL(context.Background())
	require.Nil(t, skills)
}
