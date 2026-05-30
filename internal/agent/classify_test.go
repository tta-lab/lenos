package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
