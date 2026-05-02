package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitBlocks_EmptyInput(t *testing.T) {
	t.Parallel()
	require.Nil(t, SplitBlocks(nil))
	require.Nil(t, SplitBlocks([]byte("")))
	require.Nil(t, SplitBlocks([]byte("\n\n  \n")))
}

func TestSplitBlocks_FullSession(t *testing.T) {
	t.Parallel()
	src := []byte("**λ** hi\n" +
		"\n" +
		"```bash\n" +
		"echo ok\n" +
		"```\n" +
		"\n" +
		"```\n" +
		"ok\n" +
		"```\n" +
		"\n" +
		"*[10:00:00, 1s]*\n" +
		"\n" +
		"*(turn ended)*\n" +
		"\n" +
		"> *runtime: ⚠️ note*\n")

	blocks := SplitBlocks(src)
	require.Len(t, blocks, 6, "one block per logical unit")
	assert.Equal(t, BlockUserMsg, blocks[0].Kind)
	assert.Contains(t, blocks[0].Source, "**λ** hi")
	assert.Equal(t, BlockBashCmd, blocks[1].Kind)
	assert.Contains(t, blocks[1].Source, "echo ok")
	assert.Equal(t, BlockOutput, blocks[2].Kind)
	assert.Equal(t, BlockTrailer, blocks[3].Kind)
	assert.Equal(t, BlockTurnEnd, blocks[4].Kind)
	assert.Equal(t, BlockRuntime, blocks[5].Kind)
}

func TestSplitBlocks_FenceWithInternalBlankLine(t *testing.T) {
	t.Parallel()
	// Blank lines inside a code fence should NOT split the block.
	src := []byte("```bash\n" +
		"line 1\n" +
		"\n" +
		"line 3\n" +
		"```\n")
	blocks := SplitBlocks(src)
	require.Len(t, blocks, 1)
	assert.Equal(t, BlockBashCmd, blocks[0].Kind)
	assert.Contains(t, blocks[0].Source, "line 1")
	assert.Contains(t, blocks[0].Source, "line 3")
}

func TestSplitBlocks_BackToBackFences(t *testing.T) {
	t.Parallel()
	// Closing fence should emit immediately — even without a blank line
	// before the next fence, two distinct blocks must result.
	src := []byte("```bash\n" +
		"echo hi\n" +
		"```\n" +
		"```\n" +
		"output\n" +
		"```\n")
	blocks := SplitBlocks(src)
	require.Len(t, blocks, 2)
	assert.Equal(t, BlockBashCmd, blocks[0].Kind)
	assert.Equal(t, BlockOutput, blocks[1].Kind)
}

// Regression: the boundary check that terminates a lenos-bash composite
// must accept BOTH the canonical `\`\`\`lenos-bash` form AND the legacy
// space-separated `\`\`\` lenos-bash` form. A bug in the inline boundary
// check on absorbIntoComposite missed the space form, causing the second
// command to merge into the first composite block.
func TestSplitBlocks_LenosBashCompositeBoundary_AcceptsSpaceForm(t *testing.T) {
	t.Parallel()
	src := []byte("```lenos-bash\n" +
		"first cmd\n" +
		"```\n" +
		"\n" +
		"first output\n" +
		"\n" +
		"``` lenos-bash\n" +
		"second cmd\n" +
		"```\n")
	blocks := SplitBlocks(src)
	require.Len(t, blocks, 2, "space-form fence must terminate the prior composite")
	assert.Equal(t, BlockBashCmd, blocks[0].Kind)
	assert.Contains(t, blocks[0].Source, "first cmd")
	assert.Contains(t, blocks[0].Source, "first output", "first output absorbs into the canonical-form composite")
	assert.Equal(t, BlockBashCmd, blocks[1].Kind)
	assert.Contains(t, blocks[1].Source, "second cmd")
	assert.NotContains(t, blocks[1].Source, "first output", "second composite must start clean")
}

// isLenosBashFence is the single source of truth for recognising the
// fence opening line — both canonical and space-separated forms.
func TestIsLenosBashFence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		line string
		want bool
	}{
		{"```lenos-bash", true},
		{"``` lenos-bash", true},
		{"  ```lenos-bash  ", true},
		{"```bash", false},
		{"```", false},
		{"```lenos-bashx", true}, // permissive prefix match — same as classifier
		{"** lenos-bash **", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isLenosBashFence(tc.line))
		})
	}
}

func TestBlockID_StableAndKindAware(t *testing.T) {
	t.Parallel()
	a := Block{Kind: BlockBashCmd, Source: "```bash\necho hi\n```"}
	b := Block{Kind: BlockBashCmd, Source: "```bash\necho hi\n```"}
	c := Block{Kind: BlockOutput, Source: "```bash\necho hi\n```"} // same body, diff kind

	assert.Equal(t, a.ID(), b.ID(), "same kind+source → same id")
	assert.NotEqual(t, a.ID(), c.ID(), "kind differentiates collisions")
}
