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
