package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassify_Banned(t *testing.T) {
	t.Parallel()
	cases := []string{
		`sed -i 's/a/b/' f.txt`,
		`echo x | sed --in-place s/a/b/ f`,
		`perl -i -pe 's/a/b/' f`,
		`ls && sed -i s/a/b/ f`,
	}
	for _, in := range cases {
		cls, _ := classify(in)
		assert.Equalf(t, classifyBanned, cls, "expected banned for %q", in)
	}
}

func TestContainsToolCallPattern(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		emit string
	}{
		{"xml tool_call", "<tool_call>\n{\"name\":\"bash\"}\n</tool_call>"},
		{"xml minimax tool_call", "<minimax:tool_call>\n<invoke name=\"rg\" />\n</minimax:tool_call>"},
		{"generic invoke", "<invoke name=\"cat\">"},
		{"closing tool_call tag", "</tool_call>"},
		{"tool_call with attributes", "<tool_call name=\"foo\">"},
		{"closing invoke tag", "</invoke>"},
		{"function call tag", "<function_call>{}</function_call>"},
		{"closing function_call tag", "</function_call>"},
		{"function_call with attributes", "<function_call name=\"foo\">"},
		{"minimax closing tag", "</minimax:tool_call>"},
		{"tool use tag", "<tool_use name=\"bash\">"},
		{"invoke tag", "<invoke name=\"bash\">"},
		{"bracket tool_call", "[tool_call]{\"name\":\"bash\"}[/tool_call]"},
		{"bracket TOOL_CALL", "[TOOL_CALL]{tool => 'shell'}[/TOOL_CALL]"},
		{"bracket mixed case", "[Tool_Call]content[/Tool_Call]"},
		{"bracket opening only", "[TOOL_CALL]partial content"},
		{"bracket closing only", "[/TOOL_CALL]"},
		{"bracket function_call uppercase", "[FUNCTION_CALL]{}[/FUNCTION_CALL]"},
		{"bracket tool_use", "[tool_use]rg[/tool_use]"},
		{"bracket invoke", "[INVOKE]content[/INVOKE]"},
		{"bracket no underscore toolcall", "[TOOLCALL]content"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.True(t, containsToolCallPattern(tc.emit))
		})
	}
}

func TestContainsToolCallPatternFalsePositives(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		emit string
	}{
		{"harmless brackets", "[some other content]"},
		{"markdown link", "[click here](https://example.com)"},
		{"json array", "[1, 2, 3]"},
		{"plain text", "Here is the answer to your question."},
		{"invoke in prose", "We invoke the function by calling..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.False(t, containsToolCallPattern(tc.emit))
		})
	}
}

func TestClassify_InvalidBash(t *testing.T) {
	t.Parallel()
	cases := []string{
		`if true then`, // missing semicolon and fi
		`echo $(`,      // unclosed command sub
		`fn() {`,       // unclosed function body
	}
	for _, in := range cases {
		cls, errOut := classify(in)
		assert.Equalf(t, classifyInvalidBash, cls, "expected invalid for %q (got %v)", in, cls)
		assert.Containsf(t, errOut, "lenos-bash:", "expected mvdan parser position for %q", in)
	}
}

func TestContainsToolCallPatternBeforeInvalidBash(t *testing.T) {
	t.Parallel()
	emit := "<tool_call>\n{\"name\":\"bash\",\"arguments\":{\"command\":\"echo $(\"}}\n</tool_call>"
	require.True(t, containsToolCallPattern(emit))
}

func TestClassify_Exec(t *testing.T) {
	t.Parallel()
	cases := []string{
		`ls -la`,
		`go test ./...`,
		`echo hi && echo bye`,
		`for i in 1 2 3; do echo $i; done`,
		`# comment-only emit`, // bash treats a sole comment as valid syntax
	}
	for _, in := range cases {
		cls, _ := classify(in)
		assert.Equalf(t, classifyExec, cls, "expected exec for %q (got %v)", in, cls)
	}
}

// TestClassify_HeredocWithExit ensures the regex doesn't match when the
// literal word "exit" appears inside a heredoc body (the emit also contains
// content after the heredoc, so it's not a bare-exit emit).
