package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontmatter(t *testing.T) {
	t.Run("extracts fields from full session", func(t *testing.T) {
		data, err := os.ReadFile("testdata/full_session.md")
		require.NoError(t, err)

		fm, body, err := ParseFrontmatter(data)
		require.NoError(t, err)
		assert.Equal(t, "7d3e8a91-abcd-efgh-ijkl-mnopqrstuvwx", fm.SessionID)
		assert.Equal(t, "kestrel", fm.Agent)
		assert.Equal(t, "claude-sonnet-4-6", fm.Model)
		assert.Equal(t, "2026-04-28T10:30:00Z", fm.StartedAt)
		assert.True(t, len(body) > 0)
		assert.True(t, strings.HasPrefix(string(body), "\n**λ** Find"))
	})

	t.Run("no frontmatter returns empty and original input", func(t *testing.T) {
		data := []byte("**λ** hello\n")
		fm, body, err := ParseFrontmatter(data)
		require.NoError(t, err)
		assert.Equal(t, Frontmatter{}, fm)
		assert.Equal(t, data, body)
	})

	t.Run("handles CRLF line endings", func(t *testing.T) {
		data := []byte("---\r\nsession_id: abc\r\nagent: test\r\n---\r\nbody")
		fm, body, err := ParseFrontmatter(data)
		require.NoError(t, err)
		assert.Equal(t, "abc", fm.SessionID)
		assert.Equal(t, "test", fm.Agent)
		assert.Equal(t, []byte("body"), body)
	})
}

func TestRender(t *testing.T) {
	t.Run("full session has two anchors", func(t *testing.T) {
		data, err := os.ReadFile("testdata/full_session.md")
		require.NoError(t, err)

		r, err := Render(data, 100)
		require.NoError(t, err)
		assert.Len(t, r.Anchors, 2)
		assert.Equal(t, "Find the auth bug in src/auth.go", r.Anchors[0].HeaderText)
		assert.Equal(t, "Open a PR", r.Anchors[1].HeaderText)
		assert.Equal(t, 1, r.TurnEndCount)
	})

	t.Run("full session anchor ordering invariant", func(t *testing.T) {
		data, err := os.ReadFile("testdata/full_session.md")
		require.NoError(t, err)

		r, err := Render(data, 100)
		require.NoError(t, err)
		for i := 1; i < len(r.Anchors); i++ {
			assert.True(t, r.Anchors[i].StartLine > r.Anchors[i-1].StartLine,
				"anchor[%d].StartLine (%d) should be > anchor[%d].StartLine (%d)",
				i, r.Anchors[i].StartLine, i-1, r.Anchors[i-1].StartLine)
		}
	})

	t.Run("user message has one anchor", func(t *testing.T) {
		data, err := os.ReadFile("testdata/user_message.md")
		require.NoError(t, err)

		r, err := Render(data, 100)
		require.NoError(t, err)
		assert.Len(t, r.Anchors, 1)
		assert.Equal(t, "Find the auth bug in src/auth.go", r.Anchors[0].HeaderText)
	})

	t.Run("empty body returns zero anchors and zero turn end count", func(t *testing.T) {
		data := []byte("---\n---\n")
		r, err := Render(data, 100)
		require.NoError(t, err)
		assert.Len(t, r.Anchors, 0)
		assert.Equal(t, 0, r.TurnEndCount)
	})

	t.Run("width 60 renders without panic", func(t *testing.T) {
		data, err := os.ReadFile("testdata/full_session.md")
		require.NoError(t, err)

		r, err := Render(data, 60)
		require.NoError(t, err)
		assert.NotEmpty(t, r.Lines)
	})

	t.Run("lambda inside code block does not match", func(t *testing.T) {
		// A **λ** inside a fenced code block should not be treated as a turn header.
		data := []byte("---\n---\n```bash\necho '**λ** inside code'\n```\n**λ** real turn\n")
		r, err := Render(data, 100)
		require.NoError(t, err)
		assert.Len(t, r.Anchors, 1)
		assert.Equal(t, "real turn", r.Anchors[0].HeaderText)
	})

	t.Run("width 1 renders without panic", func(t *testing.T) {
		data, err := os.ReadFile("testdata/full_session.md")
		require.NoError(t, err)

		r, err := Render(data, 1)
		require.NoError(t, err)
		assert.NotEmpty(t, r.Lines)
	})
}
