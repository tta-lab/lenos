package transcript

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRenderFrontmatter(t *testing.T) {
	t.Parallel()

	meta := Meta{
		SessionID: "test-session-1234",
		Agent:     "kestrel",
		Model:     "claude-sonnet-4-6",
		StartedAt: time.Date(2026, 4, 28, 10, 30, 0, 0, time.UTC),
	}

	golden, err := os.ReadFile("testdata/session_start.md")
	require.NoError(t, err)

	require.Equal(t, string(golden), RenderFrontmatter(meta))
}

func TestRenderUserMessage(t *testing.T) {
	t.Parallel()

	t.Run("single line", func(t *testing.T) {
		t.Parallel()

		golden, err := os.ReadFile("testdata/user_message.md")
		require.NoError(t, err)

		require.Equal(t, string(golden), RenderUserMessage("Find the auth bug in src/auth.go"))
	})

	t.Run("multi line", func(t *testing.T) {
		t.Parallel()

		golden, err := os.ReadFile("testdata/user_message_multiline.md")
		require.NoError(t, err)

		require.Equal(t, string(golden), RenderUserMessage("line one\nline two\nline three"))
	})

	t.Run("trailing newlines trimmed", func(t *testing.T) {
		t.Parallel()

		const expected = "**λ** hello\n\n"
		require.Equal(t, expected, RenderUserMessage("hello\n\n\n"))
	})
}
