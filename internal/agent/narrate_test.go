package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNarrateInvocationWritesEventsThroughBashFunction(t *testing.T) {
	t.Parallel()
	inv, err := newNarrateInvocation("narrate --to owner <<'EOF'\nFirst\nEOF\nnarrate <<'EOF'\nSecond\nEOF", nil, nil)
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
	assert.Equal(t, "Second\n", narrations[1].Body)
	assert.Empty(t, narrations[1].To)
}
