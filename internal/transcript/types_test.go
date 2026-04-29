package transcript

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSeverityString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sev      Severity
		expected string
	}{
		{SevNormal, ""},
		{SevWarn, "⚠️ "},
		{SevError, "❌ "},
		{Severity(99), ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run("", func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, tc.sev.String())
		})
	}
}
