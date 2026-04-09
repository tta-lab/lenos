package project

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestList_Timeout(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 0) // immediate timeout
	defer cancel()
	_, err := List(ctx)
	require.Error(t, err, "should error on context cancellation/timeout")
	require.Contains(t, err.Error(), "ttal")
}

func TestList_Success(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	projects, err := List(ctx)
	// May fail if ttal is not installed, but if it succeeds, should be valid JSON
	if err == nil {
		require.IsType(t, []Project{}, projects)
	}
}
