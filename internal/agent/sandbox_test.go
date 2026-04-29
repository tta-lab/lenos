package agent

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tta-lab/temenos/client"
)

func TestBuildAllowedPaths(t *testing.T) {
	t.Run("cwd is first element with correct access", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()
		paths := BuildAllowedPaths(ctx, tmpDir, "rw")
		require.NotEmpty(t, paths)
		require.Equal(t, tmpDir, paths[0].Path)
		require.False(t, paths[0].ReadOnly)
	})

	t.Run("git-common-dir appended as rw when different from cwd/.git", func(t *testing.T) {
		// Create a git repo with a worktree.
		origDir, err := os.Getwd()
		require.NoError(t, err)
		tmpDir := t.TempDir()
		require.NoError(t, os.Chdir(tmpDir))
		defer os.Chdir(origDir)

		require.NoError(t, exec.CommandContext(context.Background(), "git", "init").Run())
		worktreeDir := filepath.Join(tmpDir, "worktree")
		require.NoError(t, os.MkdirAll(worktreeDir, 0o755))
		require.NoError(t, exec.CommandContext(context.Background(), "git", "worktree", "add", worktreeDir).Run())

		ctx := context.Background()
		paths := BuildAllowedPaths(ctx, worktreeDir, "rw")
		// Should have cwd + git dir
		require.GreaterOrEqual(t, len(paths), 2)
		// Last element should be the git dir, not read-only
		gitPath := paths[len(paths)-1]
		require.Contains(t, gitPath.Path, ".git")
		require.False(t, gitPath.ReadOnly)
	})

	t.Run("read-only access sets cwd ReadOnly=true", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()
		paths := BuildAllowedPaths(ctx, tmpDir, "ro")
		require.NotEmpty(t, paths)
		require.Equal(t, tmpDir, paths[0].Path)
		require.True(t, paths[0].ReadOnly)
	})

	t.Run("additionalReadOnlyPaths appended as read-only", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()
		otherDir := t.TempDir()
		paths := BuildAllowedPaths(ctx, tmpDir, "rw", otherDir)
		// Should have cwd + otherDir
		require.GreaterOrEqual(t, len(paths), 2)
		// Find otherDir in paths
		found := false
		for _, p := range paths {
			if p.Path == otherDir {
				found = true
				require.True(t, p.ReadOnly, "additional paths should be read-only")
			}
		}
		require.True(t, found, "additionalReadOnlyPaths should be in paths")
	})

	t.Run("deduplicates cwd from additionalReadOnlyPaths", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()
		paths := BuildAllowedPaths(ctx, tmpDir, "rw", tmpDir)
		// Should NOT have tmpDir twice
		cwdCount := 0
		for _, p := range paths {
			if p.Path == tmpDir {
				cwdCount++
			}
		}
		require.Equal(t, 1, cwdCount, "cwd should not appear twice")
	})
}

func TestResolveGitCommonDir(t *testing.T) {
	t.Run("returns empty for non-git dir", func(t *testing.T) {
		ctx := context.Background()
		tmpDir := t.TempDir()
		dir := resolveGitCommonDir(ctx, tmpDir)
		// Non-git directories return empty; BuildAllowedPaths does not append a git path.
		require.Equal(t, "", dir)
	})

	t.Run("returns actual git-common-dir for worktree", func(t *testing.T) {
		origDir, err := os.Getwd()
		require.NoError(t, err)
		tmpDir := t.TempDir()
		require.NoError(t, os.Chdir(tmpDir))
		defer os.Chdir(origDir)

		// Create a git repo with a worktree.
		require.NoError(t, exec.CommandContext(context.Background(), "git", "init").Run())
		worktreeDir := filepath.Join(tmpDir, "wt")
		require.NoError(t, os.MkdirAll(worktreeDir, 0o755))
		require.NoError(t, exec.CommandContext(context.Background(), "git", "worktree", "add", worktreeDir).Run())

		ctx := context.Background()
		dir := resolveGitCommonDir(ctx, worktreeDir)
		require.NotEqual(t, worktreeDir+"/.git", dir, "worktree should have different git-common-dir")
		require.Contains(t, dir, ".git")
	})

	t.Run("context timeout returns empty", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 0) // immediate timeout
		defer cancel()
		dir := resolveGitCommonDir(ctx, t.TempDir())
		require.Equal(t, "", dir, "should return empty on context cancellation")
	})
}

func TestResolveRunner(t *testing.T) {
	cases := []struct {
		name        string
		call        SessionAgentCall
		wantLocal   bool
		wantSandbox bool
	}{
		{
			name:        "sandbox=false → local",
			call:        SessionAgentCall{Sandbox: false},
			wantLocal:   true,
			wantSandbox: false,
		},
		{
			name:        "sandbox=true + nil client → local fallback with warn",
			call:        SessionAgentCall{Sandbox: true, SandboxClient: nil},
			wantLocal:   true,
			wantSandbox: false,
		},
		{
			name:        "sandbox=true + client → sandbox",
			call:        SessionAgentCall{Sandbox: true, SandboxClient: &client.Client{}},
			wantLocal:   false,
			wantSandbox: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Run sequentially: slog.SetDefault mutates the global logger.
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, nil))
			origDefault := slog.Default()
			slog.SetDefault(logger)

			got := resolveRunner(tc.call)

			slog.SetDefault(origDefault)

			_, isLocal := got.(LocalRunner)
			_, isSandbox := got.(SandboxRunner)
			require.Equal(t, tc.wantLocal, isLocal, "LocalRunner mismatch")
			require.Equal(t, tc.wantSandbox, isSandbox, "SandboxRunner mismatch")

			if tc.name == "sandbox=true + nil client → local fallback with warn" {
				require.Contains(t, buf.String(), "falling back to LocalRunner",
					"warning should fire when temenos unreachable")
			}
		})
	}
}
