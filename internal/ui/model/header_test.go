package model

import (
	"context"
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/tta-lab/lenos/internal/agent"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/csync"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/ui/common"
	"github.com/tta-lab/lenos/internal/ui/styles"
	"github.com/tta-lab/lenos/internal/workspace"
)

// formatTodoSegment is the building block for the compact header's `TODO x/N`
// strip. Empty inputs must collapse silently; populated inputs must report
// completed-vs-total.
func TestFormatTodoSegment(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()

	t.Run("empty todos render no segment", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, formatTodoSegment(&sty, nil))
		assert.Empty(t, formatTodoSegment(&sty, []session.Todo{}))
	})

	t.Run("counts completed vs total", func(t *testing.T) {
		t.Parallel()
		todos := []session.Todo{
			{Status: session.TodoStatusCompleted},
			{Status: session.TodoStatusCompleted},
			{Status: session.TodoStatusInProgress},
			{Status: session.TodoStatusPending},
		}
		got := ansi.Strip(formatTodoSegment(&sty, todos))
		assert.True(t, strings.HasPrefix(got, "TODO"), "label first: %q", got)
		assert.Contains(t, got, "2/4", "completed/total format")
	})

	t.Run("no completed shows 0/N", func(t *testing.T) {
		t.Parallel()
		todos := []session.Todo{
			{Status: session.TodoStatusPending},
			{Status: session.TodoStatusInProgress},
		}
		got := ansi.Strip(formatTodoSegment(&sty, todos))
		assert.Contains(t, got, "0/2")
	})
}

type headerWorkspace struct {
	testWorkspace
	agentModel workspace.AgentModel
	workingDir string
	branch     string
	jobs       []agent.BackgroundJob
}

func (w *headerWorkspace) AgentIsReady() bool {
	return true
}

func (w *headerWorkspace) AgentModel() workspace.AgentModel {
	return w.agentModel
}

func (w headerWorkspace) WorkingDir() string {
	if w.workingDir != "" {
		return w.workingDir
	}
	return w.testWorkspace.WorkingDir()
}

func (w headerWorkspace) CurrentBranch(context.Context) string {
	return w.branch
}

func (w headerWorkspace) AgentActiveBackgroundJobs(string) []agent.BackgroundJob {
	return w.jobs
}

func TestRenderHeaderDetailsUsesActiveAgentContextWindow(t *testing.T) {
	t.Parallel()

	providers := csync.NewMapFrom(map[string]config.ProviderConfig{
		"main": {
			ID: "main",
			Models: []catwalk.Model{{
				ID:            "large",
				ContextWindow: 100,
			}},
		},
	})
	cfg := &config.Config{
		Models: map[config.SelectedModelType]config.SelectedModel{
			config.SelectedModelTypeLarge: {
				Provider: "main",
				Model:    "large",
			},
		},
		Providers: providers,
		Agents: map[string]config.Agent{
			config.AgentCoder: {Model: config.SelectedModelTypeLarge},
		},
	}
	sty := styles.DefaultStyles()
	com := &common.Common{
		Workspace: &headerWorkspace{
			testWorkspace: testWorkspace{cfg: cfg},
			agentModel: workspace.AgentModel{
				CatwalkCfg: catwalk.Model{ContextWindow: 400},
			},
		},
		Styles: &sty,
	}
	sess := &session.Session{
		PromptTokens:     80,
		CompletionTokens: 4,
	}

	got := ansi.Strip(renderHeaderDetails(com, sess, false, 120, nil, nil))

	assert.Contains(t, got, "21%")
	assert.NotContains(t, got, "84%")
}

func TestRenderHeaderDetailsCollapsedUsesBaseDirBranchAndJobCount(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	com := &common.Common{
		Workspace: &headerWorkspace{
			workingDir: "/home/neil/code/projects/GuionAI/flick-backend",
			branch:     "feat/header",
			jobs: []agent.BackgroundJob{
				{ID: "1", Command: "go test ./..."},
				{ID: "2", Command: "task lint"},
			},
		},
		Styles: &sty,
	}
	sess := &session.Session{ID: "session-1"}

	got := ansi.Strip(renderHeaderDetails(com, sess, false, 120, nil, nil))

	assert.Contains(t, got, "flick-backend")
	assert.NotContains(t, got, "~/c/p/G/flick-backend")
	assert.NotContains(t, got, "/home/neil/code/projects/GuionAI/flick-backend")
	assert.Contains(t, got, "feat/header")
	assert.Contains(t, got, "2 jobs")
	assert.NotContains(t, got, "go test ./...")
}

func TestRenderHeaderDetailsOpenUsesFullPathCacheBranchAndJobDetails(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	com := &common.Common{
		Workspace: &headerWorkspace{
			workingDir: "/home/neil/code/projects/GuionAI/flick-backend",
			branch:     "feat/header",
			jobs: []agent.BackgroundJob{
				{ID: "1", Command: "go test ./..."},
				{ID: "2", Command: "task lint"},
			},
		},
		Styles: &sty,
	}
	sess := &session.Session{
		ID:                  "session-1",
		PromptTokens:        100,
		CacheReadTokens:     40,
		CacheCreationTokens: 10,
		CompletionTokens:    5,
	}

	got := ansi.Strip(renderHeaderDetails(com, sess, true, 180, nil, nil))

	assert.Contains(t, got, "flick-backend")
	assert.NotContains(t, got, "/home/neil/code/projects/GuionAI/flick-backend")
	assert.Contains(t, got, "feat/header")
	assert.NotContains(t, got, "cache")
	assert.Contains(t, got, "jobs 2: go test ./..., task lint")
}
