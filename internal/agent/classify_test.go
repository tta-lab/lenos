package agent

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassify_Exit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
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
		cls, _ := classify(ctx, in)
		assert.Equalf(t, classifyExit, cls, "expected exit for %q", in)
	}
}

// TestClassify_NotExit covers the cases where the regex must NOT trigger:
// other commands that contain "exit" in prose, args, or quoted strings.
func TestClassify_NotExit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cases := []string{
		"exitcode",                          // word starting with exit
		"echo 'exit'",                       // exit inside a quoted string
		"docker run image bash -c 'exit 1'", // exit as docker arg
		"cat <<'EOF'\nexit\nEOF\necho ok",   // exit literal in heredoc
		"exit && echo done",                 // exit as part of compound
		"exit\nls",                          // exit followed by newline+cmd
		"# exit",                            // commented exit
		"export EXIT=1",                     // env var assignment
	}
	for _, in := range cases {
		cls, _ := classify(ctx, in)
		assert.NotEqualf(t, classifyExit, cls, "expected NOT exit for %q (got %v)", in, cls)
	}
}

func TestClassify_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, in := range []string{"", "   ", "\n\t\n", "  \t  "} {
		cls, _ := classify(ctx, in)
		assert.Equalf(t, classifyEmpty, cls, "expected empty for %q", in)
	}
}

func TestClassify_Banned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cases := []string{
		`sed -i 's/a/b/' f.txt`,
		`echo x | sed --in-place s/a/b/ f`,
		`perl -i -pe 's/a/b/' f`,
		`ls && sed -i s/a/b/ f`,
	}
	for _, in := range cases {
		cls, _ := classify(ctx, in)
		assert.Equalf(t, classifyBanned, cls, "expected banned for %q", in)
	}
}

func TestClassify_InvalidBash(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("/bin/bash not available")
	}
	ctx := context.Background()
	cases := []string{
		`if true then`, // missing semicolon and fi
		`echo $(`,      // unclosed command sub
		`fn() {`,       // unclosed function body
	}
	for _, in := range cases {
		cls, errOut := classify(ctx, in)
		assert.Equalf(t, classifyInvalidBash, cls, "expected invalid for %q (got %v)", in, cls)
		assert.NotEmptyf(t, errOut, "expected bash -n stderr for %q", in)
	}
}

func TestClassify_Exec(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("/bin/bash not available")
	}
	ctx := context.Background()
	cases := []string{
		`ls -la`,
		`go test ./...`,
		`echo hi && echo bye`,
		`for i in 1 2 3; do echo $i; done`,
		`# comment-only emit`, // bash treats a sole comment as valid syntax
	}
	for _, in := range cases {
		cls, _ := classify(ctx, in)
		assert.Equalf(t, classifyExec, cls, "expected exec for %q (got %v)", in, cls)
	}
}

// TestClassify_HeredocWithExit ensures the regex doesn't match when the
// literal word "exit" appears inside a heredoc body (the emit also contains
// content after the heredoc, so it's not a bare-exit emit).
func TestClassify_HeredocWithExit(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("/bin/bash not available")
	}
	ctx := context.Background()
	emit := "cat <<'EOF'\nexit\nEOF"
	cls, _ := classify(ctx, emit)
	require.Equal(t, classifyExec, cls)
}

func TestClassify_ExecExit(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("/bin/bash not available")
	}
	ctx := context.Background()
	cases := []struct {
		name string
		emit string
	}{
		{"narrate && exit", `narrate "Hi" && exit`},
		{"narrate && exit 0", `narrate "Hi" && exit 0`},
		{"semicolon exit", `echo done ; exit`},
		{"semicolon exit no space", `echo done;exit`},
		{"or exit", `echo go || exit 1`},
		{"chained && exit", `cd /tmp && ls && exit`},
		{"trailing whitespace", "echo hi && exit   "},
		{"heredoc with exit on newline", "narrate <<'EOF'\nHi\nEOF\nexit"},
		{"multi-line cmds with trailing exit", "echo one\necho two\nexit"},
		{"heredoc with exit N on newline", "cat <<EOF\nfoo\nEOF\nexit 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cls, _ := classify(ctx, tc.emit)
			require.Equal(t, classifyExecExit, cls,
				"expected classifyExecExit for %q (got %v)", tc.emit, cls)
		})
	}
}

func TestClassify_BareExitStillBareExit(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("/bin/bash not available")
	}
	// Bare `exit` must classify as classifyExit (the emit-IS-the-exit path),
	// not classifyExecExit (run-then-exit). The two paths emit different
	// recorder events.
	ctx := context.Background()
	cls, _ := classify(ctx, "exit")
	require.Equal(t, classifyExit, cls)
	cls, _ = classify(ctx, "exit 0")
	require.Equal(t, classifyExit, cls)
}

// TestClassify_ProsePrefix locks the classify() ordering: classifyProsePrefix
// must win over classifyExecExit when the emit starts with a Title-Cased word
// AND ends with `&& exit`. If the two checks were accidentally swapped, the
// trailing-exit path would run bash before the prose gate fires.
func TestClassify_ProsePrefix(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("/bin/bash not available")
	}
	ctx := context.Background()
	cases := []struct {
		emit string
		want classifyResult
	}{
		{"Read the file", classifyProsePrefix},
		{"Now starting the task", classifyProsePrefix},
		// Critical overlap: prose-prefix must win over trailing-exit.
		{"Let me start && exit", classifyProsePrefix},
		{"Read files && exit", classifyProsePrefix},
		// Lowercase-first: goes through normally.
		{"ls -la && exit", classifyExecExit},
		{"echo done && exit", classifyExecExit},
	}
	for _, tc := range cases {
		t.Run(tc.emit, func(t *testing.T) {
			t.Parallel()
			cls, aux := classify(ctx, tc.emit)
			assert.Equal(t, tc.want, cls)
			if tc.want == classifyProsePrefix {
				assert.NotEmpty(t, aux, "classify must return the prose word via aux slot")
			}
		})
	}
}

// TestDetectProsePrefix locks the cap-letter heuristic contract. Asserts both
// the captured first word AND the full offending line — re-prompts use the
// line to quote the model's prose verbatim and show in-place conversion.
func TestDetectProsePrefix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		emit     string
		wantWord string
		wantLine string
	}{
		{"capitalized first word", "Read the file", "Read", "Read the file"},
		{"single capitalized word", "Foo", "Foo", "Foo"},
		{"all caps not detected", "FOO", "", ""},
		{"lowercase first not detected", "ls -la", "", ""},
		{"single capital letter alone", "X", "", ""},
		{"starts with digit", "0xDEADBEEF", "", ""},
		{"absolute path", "/usr/bin/Read", "", ""},
		{"comment line skipped", "# Let me try\nls /tmp", "", ""},
		{"empty leading lines", "\n\n  Read the file", "Read", "Read the file"},
		{"narrate heredoc accepted", "narrate <<'EOF'\nLet me explain.\nEOF", "", ""},
		{"empty emit", "", "", ""},
		{"only whitespace", "   \n\t  ", "", ""},
		{"multi-word prose captures full line", "Now I'll start the test", "Now", "Now I'll start the test"},
		// Known false positive: Title-Cased var assignment (e.g. Output=$(pwd)).
		// The heuristic fires on the capital letter; the re-prompt is still
		// constructive (asks model to probe with command -v Output). Documented
		// here so a future regex tightening has a regression guard.
		{"var assignment false positive", "Output=$(pwd)", "Output", "Output=$(pwd)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotWord, gotLine := detectProsePrefix(tc.emit)
			assert.Equal(t, tc.wantWord, gotWord, "firstWord")
			assert.Equal(t, tc.wantLine, gotLine, "line")
		})
	}
}
