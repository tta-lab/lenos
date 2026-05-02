package model

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/tui"
	"github.com/tta-lab/lenos/internal/ui/chat"
)

// splitLenosBashSource isolates cmd from the absorbed output so the
// renderer can format them differently.
func TestSplitLenosBashSource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		source     string
		wantCmd    string
		wantOutput string
	}{
		{
			name:       "single line cmd with output",
			source:     "```lenos-bash\ngo test ./auth\n```\n\nFAIL TestExpiry",
			wantCmd:    "go test ./auth",
			wantOutput: "FAIL TestExpiry",
		},
		{
			name:       "multi-line heredoc cmd",
			source:     "```lenos-bash\nnarrate <<EOF\nhello\nEOF\n```\n\nhello",
			wantCmd:    "narrate <<EOF\nhello\nEOF",
			wantOutput: "hello",
		},
		{
			name:       "cmd with no output",
			source:     "```lenos-bash\nls\n```",
			wantCmd:    "ls",
			wantOutput: "",
		},
		{
			name:       "fence missing close — best-effort cmd, empty output",
			source:     "```lenos-bash\noops",
			wantCmd:    "oops",
			wantOutput: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd, out := splitLenosBashSource(tc.source)
			assert.Equal(t, tc.wantCmd, cmd, "cmd")
			assert.Equal(t, tc.wantOutput, out, "output")
		})
	}
}

// classifyAndRenderBlock routes lenos-bash blocks to the special renderer
// and emits a `$ <first-line>` prefix; everything else goes through Glamour.
//
// Subtests do not run in parallel — Glamour's TermRenderer mutates internal
// state during Render and is not concurrent-safe; sharing one across parallel
// subtests panics in BlockStack.Parent.
func TestClassifyAndRenderBlock_LenosBash(t *testing.T) {
	renderer, err := tui.MarkdownRenderer(80)
	require.NoError(t, err)

	t.Run("single-line cmd renders as $ cmd", func(t *testing.T) {
		b := tui.Block{Kind: tui.BlockBashCmd, Source: "```lenos-bash\nls -la\n```\n\nfile1\nfile2"}
		kind, rendered := classifyAndRenderBlock(b, renderer, nil)
		assert.Equal(t, chat.MdBlockLenosBash, kind)
		stripped := ansi.Strip(rendered)
		assert.True(t, strings.HasPrefix(stripped, "$ ls -la"), "want `$ ls -la` prefix, got %q", stripped)
		assert.Contains(t, stripped, "file1", "output absorbed into rendered body")
	})

	t.Run("multi-line cmd collapses to first line plus ellipsis", func(t *testing.T) {
		b := tui.Block{Kind: tui.BlockBashCmd, Source: "```lenos-bash\nnarrate <<EOF\nfoo\nEOF\n```"}
		kind, rendered := classifyAndRenderBlock(b, renderer, nil)
		assert.Equal(t, chat.MdBlockLenosBash, kind)
		stripped := ansi.Strip(rendered)
		assert.True(t, strings.HasPrefix(stripped, "$ narrate <<EOF"), "first line of cmd kept")
		assert.Contains(t, stripped, "…", "ellipsis hints at hidden lines")
		assert.NotContains(t, stripped, "EOF\n", "extra cmd lines stripped")
	})

	t.Run("legacy bash fence falls through to Other (Glamour)", func(t *testing.T) {
		b := tui.Block{Kind: tui.BlockBashCmd, Source: "```bash\nlegacy\n```"}
		kind, _ := classifyAndRenderBlock(b, renderer, nil)
		assert.Equal(t, chat.MdBlockOther, kind, "legacy ```bash must not become MdBlockLenosBash")
	})

	t.Run("user msg classifies as MdBlockUserMsg", func(t *testing.T) {
		b := tui.Block{Kind: tui.BlockUserMsg, Source: "**λ** hello"}
		kind, _ := classifyAndRenderBlock(b, renderer, nil)
		assert.Equal(t, chat.MdBlockUserMsg, kind)
	})
}
