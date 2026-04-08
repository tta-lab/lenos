package filepathext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContainedJoin(t *testing.T) {
	root := t.TempDir()
	// Canonicalize root so expected paths match on macOS where /var/folders → /private/var/folders.
	root, _ = filepath.EvalSymlinks(root)

	tests := []struct {
		name        string
		userPath    string
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name:     "relative file in root",
			userPath: "foo.txt",
			want:     filepath.Join(root, "foo.txt"),
			wantErr:  false,
		},
		{
			name:     "relative file in subdirectory",
			userPath: "sub/dir/foo.txt",
			want:     filepath.Join(root, "sub/dir/foo.txt"),
			wantErr:  false,
		},
		{
			name:     "absolute path inside root",
			userPath: filepath.Join(root, "bar.txt"),
			want:     filepath.Join(root, "bar.txt"),
			wantErr:  false,
		},
		{
			name:     "path with parent traversal still inside",
			userPath: "sub/../foo.txt",
			want:     filepath.Join(root, "foo.txt"),
			wantErr:  false,
		},
		{
			name:     "current directory marker",
			userPath: ".",
			want:     root,
			wantErr:  false,
		},
		{
			name:        "escapes via parent directory",
			userPath:    "../other/foo.txt",
			want:        "",
			wantErr:     true,
			errContains: "escapes",
		},
		{
			name:        "escapes deeply via parent directories",
			userPath:    "../../etc/passwd",
			want:        "",
			wantErr:     true,
			errContains: "escapes",
		},
		{
			name:        "absolute path outside root",
			userPath:    "/etc/passwd",
			want:        "",
			wantErr:     true,
			errContains: "escapes",
		},
		{
			name:        "escapes via multiple parent traversals",
			userPath:    "sub/../../foo.txt",
			want:        "",
			wantErr:     true,
			errContains: "escapes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ContainedJoin(root, tt.userPath)
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestContainedJoin_SymlinkedRoot(t *testing.T) {
	// Create a real directory and a symlink pointing to it.
	// macOS /tmp is a symlink to /private/tmp, so this tests that case.
	realDir, _ := filepath.EvalSymlinks(t.TempDir())
	linkDir := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(realDir, linkDir))

	t.Run("symlink root with relative path", func(t *testing.T) {
		got, err := ContainedJoin(linkDir, "foo.txt")
		require.NoError(t, err)
		// Result should be inside realDir (the canonical root)
		canonical, err := filepath.EvalSymlinks(filepath.Dir(got))
		require.NoError(t, err)
		realCanonical, err := filepath.EvalSymlinks(realDir)
		require.NoError(t, err)
		require.Equal(t, realCanonical, canonical)
	})

	t.Run("symlink root with absolute path inside canonical root", func(t *testing.T) {
		absInside := filepath.Join(realDir, "bar.txt")
		got, err := ContainedJoin(linkDir, absInside)
		require.NoError(t, err)
		require.Equal(t, absInside, got)
	})

	t.Run("real root with symlink absolute path", func(t *testing.T) {
		// Agent supplies a symlinked absolute path; we resolve it.
		// The result must end up inside realDir.
		got, err := ContainedJoin(realDir, filepath.Join(linkDir, "baz.txt"))
		require.NoError(t, err)
		// Verify got is inside realDir by checking prefix.
		rel, err := filepath.Rel(realDir, filepath.Dir(got))
		require.NoError(t, err)
		require.True(t, rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)),
			"got %q should be inside realDir %q, rel=%q", got, realDir, rel)
	})
}
