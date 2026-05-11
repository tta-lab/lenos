package model

import (
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
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
}

func (w *headerWorkspace) AgentIsReady() bool {
	return true
}

func (w *headerWorkspace) AgentModel() workspace.AgentModel {
	return w.agentModel
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
