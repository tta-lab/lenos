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

func TestRenderBashBlock(t *testing.T) {
	t.Parallel()

	t.Run("basic command", func(t *testing.T) {
		t.Parallel()

		golden, err := os.ReadFile("testdata/bash_block.md")
		require.NoError(t, err)
		require.Equal(t, string(golden), RenderBashBlock("go test ./auth"))
	})

	t.Run("empty content", func(t *testing.T) {
		t.Parallel()

		golden, err := os.ReadFile("testdata/bash_block_empty.md")
		require.NoError(t, err)
		require.Equal(t, string(golden), RenderBashBlock(""))
	})

	t.Run("unicode and emoji preserved", func(t *testing.T) {
		t.Parallel()

		golden, err := os.ReadFile("testdata/bash_block_unicode.md")
		require.NoError(t, err)
		require.Equal(t, string(golden), RenderBashBlock(`log warn "❌ ⚠️ λ unicode test: 你好 👨‍🦰"`))
	})

	t.Run("multi-line heredoc preserved", func(t *testing.T) {
		t.Parallel()

		heredoc := "cat <<'EOF'\nline one\nline two\nEOF"
		result := RenderBashBlock(heredoc)
		require.Contains(t, result, "cat <<'EOF'\nline one\nline two\nEOF")
	})
}

func TestHumanizeDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		duration time.Duration
		expected string
	}{
		{0, "0.000s"},
		{50 * time.Millisecond, "0.050s"},
		{150 * time.Millisecond, "0.150s"},
		{400 * time.Millisecond, "0.400s"},
		{999 * time.Millisecond, "0.999s"},
		{1 * time.Second, "1s"},
		{12 * time.Second, "12s"},
		{60 * time.Second, "1m0s"},
		{65 * time.Second, "1m5s"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.expected, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, humanizeDuration(tc.duration))
		})
	}
}

func TestRenderTrailerSuccess(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 4, 28, 10, 30, 5, 0, time.UTC)
	dur := 12 * time.Second

	golden, err := os.ReadFile("testdata/trailer_success.md")
	require.NoError(t, err)
	require.Equal(t, string(golden), RenderTrailerSuccess(base, dur))
}

func TestRenderTrailerFailure(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 4, 28, 10, 30, 5, 0, time.UTC)

	t.Run("exit 1", func(t *testing.T) {
		t.Parallel()

		golden, err := os.ReadFile("testdata/trailer_failure_exit1.md")
		require.NoError(t, err)
		require.Equal(t, string(golden), RenderTrailerFailure(base, 12*time.Second, 1))
	})

	t.Run("exit 130 SIGINT", func(t *testing.T) {
		t.Parallel()

		golden, err := os.ReadFile("testdata/trailer_failure_exit130_sigint.md")
		require.NoError(t, err)
		require.Equal(t, string(golden), RenderTrailerFailure(base, 400*time.Millisecond, 130))
	})

	t.Run("exit 137 killed", func(t *testing.T) {
		t.Parallel()

		result := RenderTrailerFailure(base, 30*time.Second, 137)
		require.Contains(t, result, "exit 137")
		require.Contains(t, result, "killed")
	})

	t.Run("exit 2 no context", func(t *testing.T) {
		t.Parallel()

		result := RenderTrailerFailure(base, 8*time.Second, 2)
		require.Contains(t, result, "exit 2")
		require.NotContains(t, result, "(")
	})
}
