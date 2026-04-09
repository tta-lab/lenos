package backend

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tta-lab/lenos/internal/config"
)

// SubscribeEvents returns the event channel for a workspace's app.
func (b *Backend) SubscribeEvents(workspaceID string) (<-chan tea.Msg, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	return ws.Events(), nil
}

// GetWorkspaceConfig returns the workspace-level configuration.
func (b *Backend) GetWorkspaceConfig(workspaceID string) (*config.Config, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	return ws.Cfg.Config(), nil
}

// GetWorkspaceProviders returns the configured providers for a
// workspace.
func (b *Backend) GetWorkspaceProviders(workspaceID string) (any, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	providers, _ := config.Providers(ws.Cfg.Config())
	return providers, nil
}
