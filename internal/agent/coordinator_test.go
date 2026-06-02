package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveSandbox(t *testing.T) {
	t.Parallel()

	t.Run("defaults true when nil", func(t *testing.T) {
		t.Parallel()
		assert.True(t, resolveSandbox(nil))
	})

	t.Run("returns true when set true", func(t *testing.T) {
		t.Parallel()
		b := true
		assert.True(t, resolveSandbox(&b))
	})

	t.Run("returns false when set false", func(t *testing.T) {
		t.Parallel()
		b := false
		assert.False(t, resolveSandbox(&b))
	})
}
