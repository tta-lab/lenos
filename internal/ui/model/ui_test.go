package model

import (
	"context"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/csync"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/ui/common"
	"github.com/tta-lab/lenos/internal/ui/styles"
	"github.com/tta-lab/lenos/internal/workspace"
)

func TestCurrentModelSupportsImages(t *testing.T) {
	t.Parallel()

	t.Run("returns false when config is nil", func(t *testing.T) {
		t.Parallel()

		ui := newTestUIWithConfig(t, nil)
		require.False(t, ui.currentModelSupportsImages())
	})

	t.Run("returns false when coder agent is missing", func(t *testing.T) {
		t.Parallel()

		cfg := &config.Config{
			Providers: csync.NewMap[string, config.ProviderConfig](),
			Agents:    map[string]config.Agent{},
		}
		ui := newTestUIWithConfig(t, cfg)
		require.False(t, ui.currentModelSupportsImages())
	})

	t.Run("returns false when model is not found", func(t *testing.T) {
		t.Parallel()

		cfg := &config.Config{
			Providers: csync.NewMap[string, config.ProviderConfig](),
			Agents: map[string]config.Agent{
				config.AgentCoder: {Model: config.SelectedModelTypeLarge},
			},
		}
		ui := newTestUIWithConfig(t, cfg)
		require.False(t, ui.currentModelSupportsImages())
	})

	t.Run("returns true when current model supports images", func(t *testing.T) {
		t.Parallel()

		providers := csync.NewMap[string, config.ProviderConfig]()
		providers.Set("test-provider", config.ProviderConfig{
			ID: "test-provider",
			Models: []catwalk.Model{
				{ID: "test-model", SupportsImages: true},
			},
		})

		cfg := &config.Config{
			Models: map[config.SelectedModelType]config.SelectedModel{
				config.SelectedModelTypeLarge: {
					Provider: "test-provider",
					Model:    "test-model",
				},
			},
			Providers: providers,
			Agents: map[string]config.Agent{
				config.AgentCoder: {Model: config.SelectedModelTypeLarge},
			},
		}

		ui := newTestUIWithConfig(t, cfg)
		require.True(t, ui.currentModelSupportsImages())
	})
}

func ptr[T any](v T) *T { return &v }

func newTestUIWithConfig(t *testing.T, cfg *config.Config) *UI {
	t.Helper()

	return &UI{
		com: &common.Common{
			Workspace: &testWorkspace{cfg: cfg},
			Styles:    ptr(styles.DefaultStyles()),
		},
	}
}

// testWorkspace is a minimal [workspace.Workspace] stub for unit tests.
type testWorkspace struct {
	workspace.Workspace
	cfg            *config.Config
	gitWorktree    bool
	modifiedFiles  []workspace.ModifiedFile
	listModifiedFn func() ([]workspace.ModifiedFile, error)
}

func (w *testWorkspace) Config() *config.Config {
	return w.cfg
}

func (w *testWorkspace) WorkingDir() string {
	return "/tmp"
}

func (w *testWorkspace) IsGitWorktree(ctx context.Context) bool {
	return w.gitWorktree
}

func (w *testWorkspace) ListModifiedFiles(ctx context.Context) ([]workspace.ModifiedFile, error) {
	if w.listModifiedFn != nil {
		return w.listModifiedFn()
	}
	return w.modifiedFiles, nil
}

func TestEffectiveTodos(t *testing.T) {
	t.Parallel()

	t.Run("returns nil when no TW job is active", func(t *testing.T) {
		t.Parallel()
		ui := newTestUIWithConfig(t, nil)
		ui.twJobID = ""
		ui.twTodos = nil
		ui.session = nil
		require.Nil(t, ui.effectiveTodos())
	})

	t.Run("returns TW subtasks when TW job is active", func(t *testing.T) {
		t.Parallel()
		ui := newTestUIWithConfig(t, nil)
		ui.twJobID = "abc123"
		ui.twTodos = []session.Todo{
			{Content: "step one", Status: session.TodoStatusCompleted},
			{Content: "step two", Status: session.TodoStatusPending},
		}
		got := ui.effectiveTodos()
		require.Len(t, got, 2)
		require.Equal(t, session.TodoStatusCompleted, got[0].Status)
		require.Equal(t, "step one", got[0].Content)
	})
}

func TestTWTickReArm(t *testing.T) {
	t.Parallel()

	t.Run("twPollMsg re-arms the ticker and returns a non-nil command", func(t *testing.T) {
		t.Parallel()
		ui := newTestUIWithConfig(t, nil)
		ui.twPollTicker = time.NewTicker(time.Hour)
		ui.twJobID = "abc123"
		ui.twTodos = nil
		ui.session = nil

		_, cmds := ui.Update(twPollMsg{todos: nil})
		require.NotNil(t, cmds, "Update should return a re-arm command after twPollMsg")
	})

	t.Run("waitNextTWTick returns nil when ticker is nil", func(t *testing.T) {
		t.Parallel()
		ui := newTestUIWithConfig(t, nil)
		ui.twPollTicker = nil
		ui.twJobID = "abc123"

		cmd := ui.waitNextTWTick()
		require.Nil(t, cmd)
	})

	t.Run("waitNextTWTick returns nil when jobID is empty", func(t *testing.T) {
		t.Parallel()
		ui := newTestUIWithConfig(t, nil)
		ui.twPollTicker = time.NewTicker(time.Hour)
		ui.twJobID = ""

		cmd := ui.waitNextTWTick()
		require.Nil(t, cmd)
	})
}

func TestGitTickReArm(t *testing.T) {
	t.Parallel()

	t.Run("gitPollMsg re-arms the ticker and returns a non-nil command", func(t *testing.T) {
		t.Parallel()
		ui := newTestUIWithConfig(t, nil)
		ui.gitPollTicker = time.NewTicker(time.Hour)
		ui.gitWorktree = true
		ui.session = nil

		_, cmds := ui.Update(gitPollMsg{files: []workspace.ModifiedFile{
			{Path: "foo.go", Added: 3, Deleted: 1},
			{Path: "bar.go", Added: 0, Deleted: 7},
		}})
		require.NotNil(t, cmds, "Update should return a re-arm command after gitPollMsg")
	})

	t.Run("waitNextGitTick returns nil when ticker is nil", func(t *testing.T) {
		t.Parallel()
		ui := newTestUIWithConfig(t, nil)
		ui.gitPollTicker = nil

		cmd := ui.waitNextGitTick()
		require.Nil(t, cmd)
	})

	t.Run("startGitPoll returns nil when not in git worktree", func(t *testing.T) {
		t.Parallel()
		ui := newTestUIWithConfig(t, nil)
		ui.gitWorktree = false

		cmd := ui.startGitPoll()
		require.Nil(t, cmd)
		require.Nil(t, ui.gitPollTicker)
	})

	t.Run("startGitPoll starts ticker when in git worktree", func(t *testing.T) {
		t.Parallel()
		ui := newTestUIWithConfig(t, nil)
		ui.gitWorktree = true

		cmd := ui.startGitPoll()
		require.NotNil(t, cmd)
		require.NotNil(t, ui.gitPollTicker)
		ui.gitPollTicker.Stop()
	})
}

func TestModifiedFilesInfo(t *testing.T) {
	t.Parallel()

	t.Run("returns empty when not in git worktree", func(t *testing.T) {
		t.Parallel()
		ui := newTestUIWithConfig(t, nil)
		ui.gitWorktree = false

		got := ui.modifiedFilesInfo(40, 10, false)
		require.Equal(t, "", got)
	})

	t.Run("returns empty in section mode when not in git worktree", func(t *testing.T) {
		t.Parallel()
		ui := newTestUIWithConfig(t, nil)
		ui.gitWorktree = false

		got := ui.modifiedFilesInfo(40, 10, true)
		require.Equal(t, "", got)
	})

	t.Run("returns None when in git worktree but no modified files", func(t *testing.T) {
		t.Parallel()
		ui := newTestUIWithConfig(t, nil)
		ui.gitWorktree = true
		ui.modifiedFiles = nil

		got := ui.modifiedFilesInfo(40, 10, false)
		require.NotEqual(t, "", got)
	})

	t.Run("returns files when modified files exist", func(t *testing.T) {
		t.Parallel()
		ui := newTestUIWithConfig(t, nil)
		ui.gitWorktree = true
		ui.modifiedFiles = []workspace.ModifiedFile{
			{Path: "foo.go", Added: 5, Deleted: 2},
			{Path: "bar.go", Added: 1, Deleted: 0},
		}

		got := ui.modifiedFilesInfo(40, 10, false)
		require.NotEqual(t, "", got)
	})
}

func TestGitPollingIntegration(t *testing.T) {
	t.Parallel()

	t.Run("gitPollMsg updates modifiedFiles and re-arms", func(t *testing.T) {
		t.Parallel()
		ui := newTestUIWithConfig(t, nil)
		ui.gitPollTicker = time.NewTicker(time.Hour)
		ui.gitWorktree = true
		ui.session = nil

		require.Nil(t, ui.modifiedFiles)

		_, cmds := ui.Update(gitPollMsg{files: []workspace.ModifiedFile{{Path: "changed.go", Added: 3, Deleted: 1}}})
		require.Equal(t, []workspace.ModifiedFile{{Path: "changed.go", Added: 3, Deleted: 1}}, ui.modifiedFiles)
		require.NotNil(t, cmds)
	})
}
