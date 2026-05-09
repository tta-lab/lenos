package transcript

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

// Mid-write transcripts (watcher delivers partial bytes between fsnotify
// events) end with an unclosed lenos-bash fence. The parser must still
// surface the partial content as a BlockBashCmd so the in-progress cmd
// renders in the chat list — silently dropping it would leave the user
// staring at a frozen UI mid-emit.
func TestSplitBlocks_UnclosedLenosBashFence(t *testing.T) {
	t.Parallel()
	src := []byte("```lenos-bash\n" +
		"narrate <<'EOF'\n" +
		"work in progress\n")
	blocks := SplitBlocks(src)
	require.Len(t, blocks, 1, "partial content must surface as a single block, not be dropped")
	assert.Equal(t, BlockBashCmd, blocks[0].Kind)
	assert.Contains(t, blocks[0].Source, "narrate <<'EOF'")
	assert.Contains(t, blocks[0].Source, "work in progress")
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

func TestSplitBlocks_MdMessages(t *testing.T) {
	t.Parallel()

	// Bare :md — message to owner.
	src := []byte(":md\nHello, world.\n")
	blocks := SplitBlocks(src)
	require.Len(t, blocks, 1)
	assert.Equal(t, BlockMdMessage, blocks[0].Kind, "bare :md must classify as BlockMdMessage")
	assert.Contains(t, blocks[0].Source, ":md")

	// :md @agent — message to a specific agent.
	src = []byte(":md @mira\nHey, review the PR?\n")
	blocks = SplitBlocks(src)
	require.Len(t, blocks, 1)
	assert.Equal(t, BlockMdMessage, blocks[0].Kind, ":md @mira must classify as BlockMdMessage")
	assert.Contains(t, blocks[0].Source, "@mira")

	// :md with multi-line body.
	src = []byte(":md\nLine one\nLine two\nLine three\n")
	blocks = SplitBlocks(src)
	require.Len(t, blocks, 1)
	assert.Equal(t, BlockMdMessage, blocks[0].Kind, "multi-line :md must classify as BlockMdMessage")
	assert.Contains(t, blocks[0].Source, "Line one")
	assert.Contains(t, blocks[0].Source, "Line three")

	// :md in a mixed transcript.
	src = []byte("**λ** hi\n\n" +
		":md\nsome message\n\n" +
		"```bash\necho ok\n```\n\n" +
		":md @mira\nanother\n")
	blocks = SplitBlocks(src)
	require.Len(t, blocks, 4, "mixed transcript: user msg, :md, bash, :md")
	assert.Equal(t, BlockUserMsg, blocks[0].Kind)
	assert.Equal(t, BlockMdMessage, blocks[1].Kind, ":md block in mixed transcript")
	assert.Equal(t, BlockBashCmd, blocks[2].Kind)
	assert.Equal(t, BlockMdMessage, blocks[3].Kind, "second :md block in mixed transcript")

	// Prose without :md prefix classifies as BlockProse.
	src = []byte("Hello, just some prose.\nNo :md prefix here.\n")
	blocks = SplitBlocks(src)
	require.Len(t, blocks, 1)
	assert.Equal(t, BlockProse, blocks[0].Kind, "text without :md prefix must classify as BlockProse")
}

func TestParseMdAddressee_EdgeCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		want string
	}{
		{"empty string", "", ""},
		{"bare :md", ":md", ""},
		{"bare :md with body", ":md\nhello world", ""},
		{":md @mira", ":md @mira", "mira"},
		{":md @mira with body", ":md @mira\nhello world", "mira"},
		{":md @ with no name", ":md @", ""},
		{"text without :md", "hello world", ""},
		{"λ user msg not :md", "**λ** hi", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ParseMdAddressee(tc.text))
		})
	}
}

func TestIsCompositeBoundary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		kind BlockKind
		text string
		want bool
	}{
		{"BlockUserMsg", BlockUserMsg, "", true},
		{"BlockRuntime", BlockRuntime, "", true},
		{"BlockTurnEnd", BlockTurnEnd, "", true},
		{"BlockMdMessage", BlockMdMessage, ":md\nhello", true},
		{"BlockBashCmd_lenosbash", BlockBashCmd, "```lenos-bash", true},
		{"BlockBashCmd_plain", BlockBashCmd, "```bash\necho hi", false},
		{"BlockOutput", BlockOutput, "some output", false},
		{"BlockProse", BlockProse, "some prose", false},
		{"BlockTrailer", BlockTrailer, "*trailer*", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isCompositeBoundary(tc.kind, tc.text))
		})
	}
}

func TestSplitBlocks_LenosBashThenMdMessage(t *testing.T) {
	t.Parallel()
	src := []byte("```lenos-bash\necho ok\n```\n\noutput\n\n:md\nhello\n")
	blocks := SplitBlocks(src)
	require.Len(t, blocks, 2, "lenos-bash block followed by :md block must produce 2 blocks")
	assert.Equal(t, BlockBashCmd, blocks[0].Kind, "first block is bash")
	assert.Equal(t, BlockMdMessage, blocks[1].Kind, "second block is :md message")
}
