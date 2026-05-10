package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNarrateInvocationWritesEventsThroughBashFunction(t *testing.T) {
	t.Parallel()
	inv, err := newNarrateInvocation("narrate --to owner <<'EOF'\nFirst\nEOF\nnarrate --continue <<'EOF'\nSecond\nEOF", nil, nil)
	require.NoError(t, err)
	defer inv.cleanup()

	res := LocalRunner{}.Run(context.Background(), inv.bash, inv.env, inv.paths)
	require.NoError(t, res.Err)
	require.Equal(t, 0, res.ExitCode, "stderr=%s", string(res.Stderr))

	narrations, err := readNarrationEvents(inv.dir)
	require.NoError(t, err)
	require.Len(t, narrations, 2)
	assert.Equal(t, "First\n", narrations[0].Body)
	assert.Equal(t, "owner", narrations[0].To)
	assert.False(t, narrations[0].Continue)
	assert.Equal(t, "Second\n", narrations[1].Body)
	assert.Empty(t, narrations[1].To)
	assert.True(t, narrations[1].Continue)
}

func TestNarrateInvocationAcceptsToAndContinueInEitherOrder(t *testing.T) {
	t.Parallel()
	inv, err := newNarrateInvocation("narrate --continue --to owner <<'EOF'\nFirst\nEOF\nnarrate --to reviewer --continue <<'EOF'\nSecond\nEOF", nil, nil)
	require.NoError(t, err)
	defer inv.cleanup()

	res := LocalRunner{}.Run(context.Background(), inv.bash, inv.env, inv.paths)
	require.NoError(t, res.Err)
	require.Equal(t, 0, res.ExitCode, "stderr=%s", string(res.Stderr))

	narrations, err := readNarrationEvents(inv.dir)
	require.NoError(t, err)
	require.Len(t, narrations, 2)
	assert.Equal(t, "First\n", narrations[0].Body)
	assert.Equal(t, "owner", narrations[0].To)
	assert.True(t, narrations[0].Continue)
	assert.Equal(t, "Second\n", narrations[1].Body)
	assert.Equal(t, "reviewer", narrations[1].To)
	assert.True(t, narrations[1].Continue)
}

func TestNarrateInvocationRejectsPositionalBodyArgs(t *testing.T) {
	t.Parallel()
	inv, err := newNarrateInvocation(`narrate "Done"`, nil, nil)
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
	inv, err := newNarrateInvocation("narrate <<'EOF'\nEOF", nil, nil)
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
