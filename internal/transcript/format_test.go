package transcript

import (
	"os"
	"strings"
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

func TestRenderFrontmatterAllFields(t *testing.T) {
	t.Parallel()

	meta := Meta{
		SessionID: "test-session-1234",
		Agent:     "kestrel",
		Model:     "claude-sonnet-4-6",
		StartedAt: time.Date(2026, 4, 28, 10, 30, 0, 0, time.UTC),
		Sandbox:   "on",
		Title:     "My Test Task",
		Cwd:       "/Users/test/project",
	}

	golden, err := os.ReadFile("testdata/session_start_full.md")
	require.NoError(t, err)

	require.Equal(t, string(golden), RenderFrontmatter(meta))
}

func TestRenderFrontmatterSandboxOnly(t *testing.T) {
	t.Parallel()

	meta := Meta{
		SessionID: "test-session-1234",
		Agent:     "kestrel",
		Model:     "claude-sonnet-4-6",
		StartedAt: time.Date(2026, 4, 28, 10, 30, 0, 0, time.UTC),
		Sandbox:   "on",
	}

	output := RenderFrontmatter(meta)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	require.Equal(t, 7, len(lines))
	require.Contains(t, output, "sandbox: on\n")
	require.NotContains(t, output, "title:")
	require.NotContains(t, output, "cwd:")
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
		require.Equal(t, string(golden), RenderBashBlock(`narrate "❌ ⚠️ λ unicode test: 你好 👨‍🦰"`))
	})

	t.Run("multi-line heredoc preserved", func(t *testing.T) {
		t.Parallel()

		heredoc := "cat <<'EOF'\nline one\nline two\nEOF"
		result := RenderBashBlock(heredoc)
		require.Contains(t, result, "cat <<'EOF'\nline one\nline two\nEOF")
	})
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

	t.Run("exit 143 SIGTERM", func(t *testing.T) {
		t.Parallel()

		result := RenderTrailerFailure(base, 5*time.Second, 143)
		require.Contains(t, result, "exit 143")
		require.Contains(t, result, "SIGTERM")
	})

	t.Run("exit 2 no context", func(t *testing.T) {
		t.Parallel()

		result := RenderTrailerFailure(base, 8*time.Second, 2)
		require.Contains(t, result, "exit 2")
		require.NotContains(t, result, "(")
	})
}

func TestRenderOutputBlock(t *testing.T) {
	t.Parallel()

	t.Run("basic output", func(t *testing.T) {
		t.Parallel()

		golden, err := os.ReadFile("testdata/output_block.md")
		require.NoError(t, err)
		out := []byte("expected: 2026-01-01\ngot:      2025-12-31\nFAIL TestAuthExpiry")
		require.Equal(t, string(golden), RenderOutputBlock(out))
	})

	t.Run("empty output", func(t *testing.T) {
		t.Parallel()

		result := RenderOutputBlock(nil)
		require.Equal(t, "", result)
	})

	t.Run("triple-backticks sanitized to prevent fence imbalance", func(t *testing.T) {
		t.Parallel()

		out := []byte("foo\n```\nbar")
		got := RenderOutputBlock(out)
		require.NotContains(t, got, "```")
		require.Contains(t, got, "bar")
	})

	t.Run("unicode and emoji preserved", func(t *testing.T) {
		t.Parallel()

		golden, err := os.ReadFile("testdata/output_block_unicode.md")
		require.NoError(t, err)
		out := []byte("❌ build failed\n⚠️ warning: deprecated API\nλ unicode 你好 👨‍🦰")
		require.Equal(t, string(golden), RenderOutputBlock(out))
	})
}

func TestRenderRuntimeEvent(t *testing.T) {
	t.Parallel()

	// Explicitly assert severity prefixes to catch regressions.
	t.Run("SevNormal prefix empty", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "", SevNormal.String())
	})
	t.Run("SevWarn prefix emoji", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "⚠️ ", SevWarn.String())
	})
	t.Run("SevError prefix emoji", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "❌ ", SevError.String())
	})

	t.Run("normal", func(t *testing.T) {
		t.Parallel()

		golden, err := os.ReadFile("testdata/runtime_event_normal.md")
		require.NoError(t, err)
		require.Equal(t, string(golden), RenderRuntimeEvent(SevNormal, "blocked: sed -i not allowed; use src edit"))
	})

	t.Run("warn", func(t *testing.T) {
		t.Parallel()

		golden, err := os.ReadFile("testdata/runtime_event_warn.md")
		require.NoError(t, err)
		require.Equal(t, string(golden), RenderRuntimeEvent(SevWarn, "timeout after 120s; subprocess killed"))
	})

	t.Run("error", func(t *testing.T) {
		t.Parallel()

		golden, err := os.ReadFile("testdata/runtime_event_error.md")
		require.NoError(t, err)
		require.Equal(t, string(golden), RenderRuntimeEvent(SevError, "sqlite write failed: disk full"))
	})

	t.Run("long unicode description", func(t *testing.T) {
		t.Parallel()

		result := RenderRuntimeEvent(SevWarn, "测试长描述 with emoji 👨‍🦰⚠️")
		require.Contains(t, result, "👨‍🦰⚠️")
		require.Contains(t, result, "测试长描述")
	})
}

func TestRenderTurnEnd(t *testing.T) {
	t.Parallel()

	golden, err := os.ReadFile("testdata/turn_end.md")
	require.NoError(t, err)
	require.Equal(t, string(golden), RenderTurnEnd())
}

func TestRenderProse(t *testing.T) {
	t.Parallel()

	t.Run("basic prose", func(t *testing.T) {
		t.Parallel()

		golden, err := os.ReadFile("testdata/prose.md")
		require.NoError(t, err)
		require.Equal(t, string(golden), RenderProse("expiry comparison is reversed — t.ExpiresAt.Before(time.Now()) should be After"))
	})

	t.Run("trailing newlines trimmed", func(t *testing.T) {
		t.Parallel()

		result := RenderProse("hello world\n\n\n")
		require.Equal(t, "hello world\n\n", result)
	})

	t.Run("multi-line with blank lines", func(t *testing.T) {
		t.Parallel()

		result := RenderProse("line one\n\nline two\n")
		require.Equal(t, "line one\n\nline two\n\n", result)
	})
}
