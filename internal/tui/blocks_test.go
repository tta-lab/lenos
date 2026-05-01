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

func TestBlockID_StableAndKindAware(t *testing.T) {
	t.Parallel()
	a := Block{Kind: BlockBashCmd, Source: "```bash\necho hi\n```"}
	b := Block{Kind: BlockBashCmd, Source: "```bash\necho hi\n```"}
	c := Block{Kind: BlockOutput, Source: "```bash\necho hi\n```"} // same body, diff kind

	assert.Equal(t, a.ID(), b.ID(), "same kind+source → same id")
	assert.NotEqual(t, a.ID(), c.ID(), "kind differentiates collisions")
}
