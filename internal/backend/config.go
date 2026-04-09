package backend

import (
	"context"

	"github.com/tta-lab/lenos/internal/agent"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/oauth"
)

// SetConfigField sets a key/value pair in the config file for the
// given scope.
func (b *Backend) SetConfigField(workspaceID string, scope config.Scope, key string, value any) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}
	return ws.Cfg.SetConfigField(scope, key, value)
}

// RemoveConfigField removes a key from the config file for the given
// scope.
func (b *Backend) RemoveConfigField(workspaceID string, scope config.Scope, key string) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}
	return ws.Cfg.RemoveConfigField(scope, key)
}

// UpdatePreferredModel updates the preferred model for the given type
// and persists it to the config file at the given scope.
func (b *Backend) UpdatePreferredModel(workspaceID string, scope config.Scope, modelType config.SelectedModelType, model config.SelectedModel) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}
	return ws.Cfg.UpdatePreferredModel(scope, modelType, model)
}

// SetCompactMode sets the compact mode setting and persists it.
func (b *Backend) SetCompactMode(workspaceID string, scope config.Scope, enabled bool) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}
	return ws.Cfg.SetCompactMode(scope, enabled)
}

// SetProviderAPIKey sets the API key for a provider and persists it.
func (b *Backend) SetProviderAPIKey(workspaceID string, scope config.Scope, providerID string, apiKey any) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}
	return ws.Cfg.SetProviderAPIKey(scope, providerID, apiKey)
}

// ImportCopilot attempts to import a GitHub Copilot token from disk.
func (b *Backend) ImportCopilot(workspaceID string) (*oauth.Token, bool, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, false, err
	}
	token, ok := ws.Cfg.ImportCopilot()
	return token, ok, nil
}

// RefreshOAuthToken refreshes the OAuth token for a provider.
func (b *Backend) RefreshOAuthToken(ctx context.Context, workspaceID string, scope config.Scope, providerID string) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}
	return ws.Cfg.RefreshOAuthToken(ctx, scope, providerID)
}

// ProjectNeedsInitialization checks whether the project in this
// workspace needs initialization.
func (b *Backend) ProjectNeedsInitialization(workspaceID string) (bool, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return false, err
	}
	return config.ProjectNeedsInitialization(ws.Cfg)
}

// MarkProjectInitialized marks the project as initialized.
func (b *Backend) MarkProjectInitialized(workspaceID string) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}
	return config.MarkProjectInitialized(ws.Cfg)
}

// InitializePrompt builds the initialization prompt for the workspace.
func (b *Backend) InitializePrompt(workspaceID string) (string, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return "", err
	}
	return agent.InitializePrompt(ws.Cfg)
}

// GetWorkingDir returns the working directory for a workspace.
func (b *Backend) GetWorkingDir(workspaceID string) (string, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return "", err
	}
	return ws.Cfg.WorkingDir(), nil
}
