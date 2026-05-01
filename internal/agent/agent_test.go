package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/session"

	_ "github.com/joho/godotenv/autoload"
)

func makeTestTodos(n int) []session.Todo {
	todos := make([]session.Todo, n)
	for i := range n {
		todos[i] = session.Todo{
			Status:  session.TodoStatusPending,
			Content: fmt.Sprintf("Task %d: Implement feature with some description that makes it realistic", i),
		}
	}
	return todos
}

func TestBuildSummaryPrompt(t *testing.T) {
	t.Parallel()

	t.Run("empty jobID returns base prompt without todos section", func(t *testing.T) {
		t.Parallel()
		result := buildSummaryPrompt(context.Background(), "")
		require.Contains(t, result, "Provide a detailed summary of our conversation above.")
		require.NotContains(t, result, "Current Todo List")
	})

	t.Run("cancelled context returns base prompt", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result := buildSummaryPrompt(ctx, "fake-nonexistent-job-id")
		require.Contains(t, result, "Provide a detailed summary of our conversation above.")
		require.NotContains(t, result, "Current Todo List")
	})

	t.Run("successful poll with real jobID returns prompt", func(t *testing.T) {
		t.Parallel()
		// Use a UUID unlikely to have subtasks in a fresh test environment.
		// task will return empty output, so todos section won't appear but
		// the base prompt will.
		result := buildSummaryPrompt(context.Background(), "00000000-0000-0000-0000-000000000000")
		require.Contains(t, result, "Provide a detailed summary of our conversation above.")
	})
}

// chdirIntoWorktree creates a tempdir shaped like
// `.../worktrees/<hex>-test` and chdirs into it so taskwarrior.ResolveJobID
// returns the hex.
func chdirIntoWorktree(t *testing.T, hex string) {
	t.Helper()
	root := t.TempDir()
	wt := filepath.Join(root, "worktrees", hex+"-test")
	require.NoError(t, os.MkdirAll(wt, 0o755))
	t.Chdir(wt)
}

func TestGenerateTitle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("task CLI not available on windows")
	}

	t.Run("uses task description as session title", func(t *testing.T) {
		chdirIntoWorktree(t, "25620b89")
		env := testEnv(t)
		sess, err := env.sessions.Create(t.Context(), "Untitled Session")
		require.NoError(t, err)

		// Create a fake "task" binary that outputs a JSON array.
		tmp := t.TempDir()
		oldPath := os.Getenv("PATH")
		t.Cleanup(func() { os.Setenv("PATH", oldPath) })
		os.Setenv("PATH", tmp+string(os.PathListSeparator)+oldPath)

		fakeTask := filepath.Join(tmp, "task")
		require.NoError(t, os.WriteFile(fakeTask, []byte(fmt.Sprintf(`#!/bin/sh
printf '[{"description":"%s","status":"pending"}]' "$@"
`, "Fix the authentication bug")), 0o755))

		a := &sessionAgent{sessions: env.sessions}
		a.generateTitle(t.Context(), sess.ID, "fix auth bug")

		updated, err := env.sessions.Get(t.Context(), sess.ID)
		require.NoError(t, err)
		assert.Equal(t, "Fix the authentication bug", updated.Title)
	})

	t.Run("empty array falls back to default", func(t *testing.T) {
		chdirIntoWorktree(t, "25620b89")
		env := testEnv(t)
		sess, err := env.sessions.Create(t.Context(), "Untitled Session")
		require.NoError(t, err)

		tmp := t.TempDir()
		oldPath := os.Getenv("PATH")
		t.Cleanup(func() { os.Setenv("PATH", oldPath) })
		os.Setenv("PATH", tmp+string(os.PathListSeparator)+oldPath)

		fakeTask := filepath.Join(tmp, "task")
		require.NoError(t, os.WriteFile(fakeTask, []byte("#!/bin/sh\nprintf '[]' \"$@\""), 0o755))

		a := &sessionAgent{sessions: env.sessions}
		a.generateTitle(t.Context(), sess.ID, "")

		updated, err := env.sessions.Get(t.Context(), sess.ID)
		require.NoError(t, err)
		assert.Equal(t, DefaultSessionName, updated.Title)
	})

	t.Run("non-worktree cwd uses default", func(t *testing.T) {
		// Plain tempdir (no worktrees/<hex>-* parent) → ResolveJobIDFromCwd
		// returns "" → default title.
		t.Chdir(t.TempDir())
		env := testEnv(t)
		sess, err := env.sessions.Create(t.Context(), "Untitled Session")
		require.NoError(t, err)

		a := &sessionAgent{sessions: env.sessions}
		a.generateTitle(t.Context(), sess.ID, "")

		updated, err := env.sessions.Get(t.Context(), sess.ID)
		require.NoError(t, err)
		assert.Equal(t, DefaultSessionName, updated.Title)
	})

	t.Run("description over 100 chars is truncated", func(t *testing.T) {
		chdirIntoWorktree(t, "25620b89")
		env := testEnv(t)
		sess, err := env.sessions.Create(t.Context(), "Untitled Session")
		require.NoError(t, err)

		tmp := t.TempDir()
		oldPath := os.Getenv("PATH")
		t.Cleanup(func() { os.Setenv("PATH", oldPath) })
		os.Setenv("PATH", tmp+string(os.PathListSeparator)+oldPath)

		longDesc := strings.Repeat("x", 150)
		fakeTask := filepath.Join(tmp, "task")
		require.NoError(t, os.WriteFile(fakeTask, []byte(fmt.Sprintf("#!/bin/sh\nprintf '[{\"description\":\"%s\",\"status\":\"pending\"}]' \"$@\"", longDesc)), 0o755))

		a := &sessionAgent{sessions: env.sessions}
		a.generateTitle(t.Context(), sess.ID, "")

		updated, err := env.sessions.Get(t.Context(), sess.ID)
		require.NoError(t, err)
		assert.Len(t, updated.Title, 100)
		assert.Equal(t, longDesc[:100], updated.Title)
	})
}

func TestShouldAutoCompact(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cw   int64
		used int64
		want bool
	}{
		{"zero context window guarded", 0, 100, false},
		{"negative context window guarded", -1, 100, false},
		{"large window, well under buffer", 200_001, 100_000, false},
		{"large window, just above buffer (remaining 20_001)", 200_001, 180_000, false},
		{"large window, exactly at buffer (remaining 20_000)", 200_001, 180_001, true},
		{"large window, over buffer", 200_001, 195_000, true},
		{"small window, well under ratio", 100_000, 50_000, false},
		{"small window, just above ratio (remaining 20_001)", 100_000, 79_999, false},
		{"small window, exactly at ratio (remaining 20_000)", 100_000, 80_000, true},
		{"small window, over ratio", 100_000, 95_000, true},
		{"boundary cw == largeContextWindowThreshold uses ratio path", 200_000, 159_999, false},
		{"boundary cw == largeContextWindowThreshold uses ratio path (over)", 200_000, 160_000, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := shouldAutoCompact(tc.cw, tc.used)
			assert.Equal(t, tc.want, got)
		})
	}
}
