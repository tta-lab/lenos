package taskwarrior

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveJobID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cwd  string
		want string
	}{
		{"worktree with alias", "/Users/neil/.ttal/worktrees/680e5b5d-len", "680e5b5d"},
		{"worktree bare hex", "/somewhere/worktrees/abcdef01", "abcdef01"},
		{"non-worktree cwd", "/Users/neil/code/myproj", ""},
		{"basename too short", "/foo/bar/abcdef0-len", ""},
		{"uppercase rejected", "/foo/bar/ABCDEF01-len", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, ResolveJobID(tc.cwd))
		})
	}
}
