package agent

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNarrateInvocationWritesEventsThroughBashFunction(t *testing.T) {
	t.Parallel()
	inv, err := newNarrateInvocation("cat <<'EOF' | narrate --to owner\nFirst\nEOF\ncat <<'EOF' | narrate --continue\nSecond\nEOF", nil, nil, "")
	require.NoError(t, err)
	defer inv.cleanup()

	res := LocalRunner{}.Run(context.Background(), inv.bash, inv.env, inv.paths)
	require.NoError(t, res.Err)
	require.Equal(t, 0, res.ExitCode, "stderr=%s", string(res.Stderr))

	narrations, err := readNarrationEvents(inv.dir)
	require.NoError(t, err)
	require.Len(t, narrations, 2)
	sort.Slice(narrations, func(i, j int) bool { return narrations[i].Body < narrations[j].Body })
	assert.Equal(t, "First\n", narrations[0].Body)
	assert.Equal(t, "owner", narrations[0].To)
	assert.False(t, narrations[0].Continue)
	assert.Equal(t, "Second\n", narrations[1].Body)
	assert.Empty(t, narrations[1].To)
	assert.True(t, narrations[1].Continue)
}

func TestNarrateInvocationAcceptsToAndContinueInEitherOrder(t *testing.T) {
	t.Parallel()
	inv, err := newNarrateInvocation("cat <<'EOF' | narrate --continue --to owner\nFirst\nEOF\ncat <<'EOF' | narrate --to reviewer --continue\nSecond\nEOF", nil, nil, "")
	require.NoError(t, err)
	defer inv.cleanup()

	res := LocalRunner{}.Run(context.Background(), inv.bash, inv.env, inv.paths)
	require.NoError(t, res.Err)
	require.Equal(t, 0, res.ExitCode, "stderr=%s", string(res.Stderr))

	narrations, err := readNarrationEvents(inv.dir)
	require.NoError(t, err)
	require.Len(t, narrations, 2)
	sort.Slice(narrations, func(i, j int) bool { return narrations[i].Body < narrations[j].Body })
	assert.Equal(t, "First\n", narrations[0].Body)
	assert.Equal(t, "owner", narrations[0].To)
	assert.True(t, narrations[0].Continue)
	assert.Equal(t, "Second\n", narrations[1].Body)
	assert.Equal(t, "reviewer", narrations[1].To)
	assert.True(t, narrations[1].Continue)
}

func TestNarrateInvocationAppliesDefaultToWhenMissing(t *testing.T) {
	t.Parallel()
	inv, err := newNarrateInvocation("cat <<'EOF' | narrate\nFirst\nEOF\ncat <<'EOF' | narrate --to reviewer\nSecond\nEOF", nil, nil, "pair")
	require.NoError(t, err)
	defer inv.cleanup()

	res := LocalRunner{}.Run(context.Background(), inv.bash, inv.env, inv.paths)
	require.NoError(t, res.Err)
	require.Equal(t, 0, res.ExitCode, "stderr=%s", string(res.Stderr))

	narrations, err := readNarrationEvents(inv.dir)
	require.NoError(t, err)
	require.Len(t, narrations, 2)
	sort.Slice(narrations, func(i, j int) bool { return narrations[i].Body < narrations[j].Body })
	assert.Equal(t, "First\n", narrations[0].Body)
	assert.Equal(t, "pair", narrations[0].To)
	assert.Equal(t, "Second\n", narrations[1].Body)
	assert.Equal(t, "reviewer", narrations[1].To)
}

func TestNarrateInvocationRejectsPositionalBodyArgs(t *testing.T) {
	t.Parallel()
	inv, err := newNarrateInvocation(`narrate "Done"`, nil, nil, "")
	require.NoError(t, err)
	defer inv.cleanup()

	res := LocalRunner{}.Run(context.Background(), inv.bash, inv.env, inv.paths)
	require.NoError(t, res.Err)
	require.NotEqual(t, 0, res.ExitCode)
	assert.Contains(t, string(res.Stderr), "stdin")

	narrations, err := readNarrationEvents(inv.dir)
	require.NoError(t, err)
	assert.Empty(t, narrations)
}

func TestNarrateInvocationRejectsEmptyBody(t *testing.T) {
	t.Parallel()
	inv, err := newNarrateInvocation("cat <<'EOF' | narrate\nEOF", nil, nil, "")
	require.NoError(t, err)
	defer inv.cleanup()

	res := LocalRunner{}.Run(context.Background(), inv.bash, inv.env, inv.paths)
	require.NoError(t, res.Err)
	require.NotEqual(t, 0, res.ExitCode)
	assert.Contains(t, string(res.Stderr), "empty")

	narrations, err := readNarrationEvents(inv.dir)
	require.NoError(t, err)
	assert.Empty(t, narrations)
}
