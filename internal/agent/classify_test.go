package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassify_Exit(t *testing.T) {
	t.Parallel()
	cases := []string{
		"exit",
		"exit 0",
		"exit 1",
		"exit -1",
		"  exit 1  ",
		"exit\t0",  // tab between exit and N is bash-legal
		"\texit\n", // leading tab + trailing newline
	}
	for _, in := range cases {
		cls, _ := classify(in)
		assert.Equalf(t, classifyExit, cls, "expected exit for %q", in)
	}
}

// TestClassify_NotExit covers the cases where the regex must NOT trigger:
// other commands that contain "exit" in prose, args, or quoted strings.
func TestClassify_NotExit(t *testing.T) {
	t.Parallel()
	cases := []string{
		"exitcode",                          // word starting with exit
		"echo 'exit'",                       // exit inside a quoted string
		"docker run image bash -c 'exit 1'", // exit as docker arg
		"cat <<'EOF'\nexit\nEOF\necho ok",   // exit literal in heredoc
		"exit && echo done",                 // exit as part of compound
		"exit\nls",                          // exit followed by newline+cmd
		":exit",                             // legacy text-mode exit is not a loop exit anymore
		":exit 0",                           // only bare bash exit exits the loop
		"# exit",                            // commented exit
		"export EXIT=1",                     // env var assignment
	}
	for _, in := range cases {
		cls, _ := classify(in)
		assert.NotEqualf(t, classifyExit, cls, "expected NOT exit for %q (got %v)", in, cls)
	}
}

func TestClassify_Empty(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "   ", "\n\t\n", "  \t  "} {
		cls, _ := classify(in)
		assert.Equalf(t, classifyEmpty, cls, "expected empty for %q", in)
	}
}

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

func TestClassify_ToolCall(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		emit string
	}{
		{"xml tool_call", "<tool_call>\n{\"name\":\"bash\"}\n</tool_call>"},
		{"xml minimax tool_call", "<minimax:tool_call>\n<invoke name=\"rg\" />\n</minimax:tool_call>"},
		{"function call tag", "<function_call>{}</function_call>"},
		{"tool use tag", "<tool_use name=\"bash\">"},
		{"invoke tag", "<invoke name=\"bash\">"},
		{"bracket tool_call", "[tool_call]{\"name\":\"bash\"}[/tool_call]"},
		{"bracket function_call uppercase", "[FUNCTION_CALL]{}[/FUNCTION_CALL]"},
		{"bracket tool_use", "[tool_use]rg[/tool_use]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cls, aux := classify(tc.emit)
			assert.Equal(t, classifyToolCall, cls)
			assert.Empty(t, aux)
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

func TestClassify_ToolCallBeatsInvalidBash(t *testing.T) {
	t.Parallel()
	emit := "<tool_call>\n{\"name\":\"bash\",\"arguments\":{\"command\":\"echo $(\"}}\n</tool_call>"
	cls, _ := classify(emit)
	require.Equal(t, classifyToolCall, cls)
}

func TestClassify_NaturalLanguage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		emit string
		want classifyResult
	}{
		{"Hello world", classifyNaturalLanguage},
		{"我已经完成了。", classifyNaturalLanguage},
		{"確認しました。", classifyNaturalLanguage},
		{"## Done", classifyNaturalLanguage},
		{"### Done", classifyNaturalLanguage},
		{"> Done", classifyNaturalLanguage},
		{"```bash\necho hi\n```", classifyNaturalLanguage},
		{"Output=$(pwd)", classifyExec},
		{"VAR=value go test ./...", classifyExec},
		{"ls -la", classifyExec},
		{"# checking\nls", classifyExec},
	}
	for _, tc := range cases {
		t.Run(tc.emit, func(t *testing.T) {
			t.Parallel()
			cls, _ := classify(tc.emit)
			require.Equal(t, tc.want, cls)
		})
	}
}

func TestClassify_NaturalLanguageFirstLineWithValidBashRestStaysNaturalLanguage(t *testing.T) {
	t.Parallel()
	emit := "I'll inspect the repo.\ncat README.md && ls"

	cls, aux := classify(emit)

	require.Equal(t, classifyNaturalLanguage, cls)
	assert.Empty(t, aux)
}

func TestClassify_NaturalLanguageMultilineCJKStaysNaturalLanguage(t *testing.T) {
	t.Parallel()
	cases := []string{
		"我已经完成了。\n不需要继续操作。",
		"確認しました。\n次の操作は不要です。",
	}

	for _, emit := range cases {
		t.Run(emit, func(t *testing.T) {
			t.Parallel()

			cls, aux := classify(emit)

			require.Equal(t, classifyNaturalLanguage, cls)
			assert.Empty(t, aux)
		})
	}
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
func TestClassify_HeredocWithExit(t *testing.T) {
	t.Parallel()
	emit := "cat <<'EOF'\nexit\nEOF"
	cls, _ := classify(emit)
	require.Equal(t, classifyExec, cls)
}

func TestClassify_TrailingExitIsExec(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		emit string
	}{
		{"printf && exit", `printf '%s\n' "Hi" && exit`},
		{"printf && exit 0", `printf '%s\n' "Hi" && exit 0`},
		{"semicolon exit", `echo done ; exit`},
		{"semicolon exit no space", `echo done;exit`},
		{"or exit", `echo go || exit 1`},
		{"chained && exit", `cd /tmp && ls && exit`},
		{"trailing whitespace", "echo hi && exit   "},
		{"heredoc with exit on newline", "cat <<'EOF'\nHi\nEOF\nexit"},
		{"multi-line cmds with trailing exit", "echo one\necho two\nexit"},
		{"heredoc with exit N on newline", "cat <<EOF\nfoo\nEOF\nexit 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cls, _ := classify(tc.emit)
			require.Equal(t, classifyExec, cls,
				"expected trailing exit to remain plain exec for %q (got %v)", tc.emit, cls)
		})
	}
}

func TestClassify_BareExitStillBareExit(t *testing.T) {
	t.Parallel()
	// Bare `exit` must classify as classifyExit (the emit-IS-the-exit path).
	cls, _ := classify("exit")
	require.Equal(t, classifyExit, cls)
	cls, _ = classify("exit 0")
	require.Equal(t, classifyExit, cls)
}

func TestClassify_NaturalLanguageReplacesTitleCaseProseHeuristic(t *testing.T) {
	t.Parallel()
	cases := []struct {
		emit string
		want classifyResult
	}{
		{"Read the file", classifyNaturalLanguage},
		{"Now starting the task", classifyNaturalLanguage},
		{"Let me start && exit", classifyNaturalLanguage},
		{"Read files && exit", classifyNaturalLanguage},
		// Lowercase-first: goes through normally.
		{"ls -la && exit", classifyExec},
		{"echo done && exit", classifyExec},
	}
	for _, tc := range cases {
		t.Run(tc.emit, func(t *testing.T) {
			t.Parallel()
			cls, _ := classify(tc.emit)
			assert.Equal(t, tc.want, cls)
		})
	}
}

func TestClassify_NaturalLanguageBeatsInvalidBash(t *testing.T) {
	t.Parallel()
	emit := "Read the file and tell me what's wrong with $('"
	cls, _ := classify(emit)
	require.Equal(t, classifyNaturalLanguage, cls)
}
