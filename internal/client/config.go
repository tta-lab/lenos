package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/oauth"
)

// SetConfigField sets a config key/value pair on the server.
func (c *Client) SetConfigField(ctx context.Context, id string, scope config.Scope, key string, value any) error {
	rsp, err := c.post(ctx, fmt.Sprintf("/workspaces/%s/config/set", id), nil, jsonBody(struct {
		Scope config.Scope `json:"scope"`
		Key   string       `json:"key"`
		Value any          `json:"value"`
	}{Scope: scope, Key: key, Value: value}), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		return fmt.Errorf("failed to set config field: %w", err)
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to set config field: status code %d", rsp.StatusCode)
	}
	return nil
}

// RemoveConfigField removes a config key on the server.
func (c *Client) RemoveConfigField(ctx context.Context, id string, scope config.Scope, key string) error {
	rsp, err := c.post(ctx, fmt.Sprintf("/workspaces/%s/config/remove", id), nil, jsonBody(struct {
		Scope config.Scope `json:"scope"`
		Key   string       `json:"key"`
	}{Scope: scope, Key: key}), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		return fmt.Errorf("failed to remove config field: %w", err)
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to remove config field: status code %d", rsp.StatusCode)
	}
	return nil
}

// UpdatePreferredModel updates the preferred model on the server.
func (c *Client) UpdatePreferredModel(ctx context.Context, id string, scope config.Scope, modelType config.SelectedModelType, model config.SelectedModel) error {
	rsp, err := c.post(ctx, fmt.Sprintf("/workspaces/%s/config/model", id), nil, jsonBody(struct {
		Scope     config.Scope             `json:"scope"`
		ModelType config.SelectedModelType `json:"model_type"`
		Model     config.SelectedModel     `json:"model"`
	}{Scope: scope, ModelType: modelType, Model: model}), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		return fmt.Errorf("failed to update preferred model: %w", err)
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to update preferred model: status code %d", rsp.StatusCode)
	}
	return nil
}

// SetCompactMode sets compact mode on the server.
func (c *Client) SetCompactMode(ctx context.Context, id string, scope config.Scope, enabled bool) error {
	rsp, err := c.post(ctx, fmt.Sprintf("/workspaces/%s/config/compact", id), nil, jsonBody(struct {
		Scope   config.Scope `json:"scope"`
		Enabled bool         `json:"enabled"`
	}{Scope: scope, Enabled: enabled}), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		return fmt.Errorf("failed to set compact mode: %w", err)
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to set compact mode: status code %d", rsp.StatusCode)
	}
	return nil
}

// SetProviderAPIKey sets a provider API key on the server.
func (c *Client) SetProviderAPIKey(ctx context.Context, id string, scope config.Scope, providerID string, apiKey any) error {
	rsp, err := c.post(ctx, fmt.Sprintf("/workspaces/%s/config/provider-key", id), nil, jsonBody(struct {
		Scope      config.Scope `json:"scope"`
		ProviderID string       `json:"provider_id"`
		APIKey     any          `json:"api_key"`
	}{Scope: scope, ProviderID: providerID, APIKey: apiKey}), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		return fmt.Errorf("failed to set provider API key: %w", err)
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to set provider API key: status code %d", rsp.StatusCode)
	}
	return nil
}

// ImportCopilot attempts to import a GitHub Copilot token on the
// server.
func (c *Client) ImportCopilot(ctx context.Context, id string) (*oauth.Token, bool, error) {
	rsp, err := c.post(ctx, fmt.Sprintf("/workspaces/%s/config/import-copilot", id), nil, nil, nil)
	if err != nil {
		return nil, false, fmt.Errorf("failed to import copilot: %w", err)
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("failed to import copilot: status code %d", rsp.StatusCode)
	}
	var result struct {
		Token   *oauth.Token `json:"token"`
		Success bool         `json:"success"`
	}
	if err := json.NewDecoder(rsp.Body).Decode(&result); err != nil {
		return nil, false, fmt.Errorf("failed to decode import copilot response: %w", err)
	}
	return result.Token, result.Success, nil
}

// RefreshOAuthToken refreshes an OAuth token for a provider on the
// server.
func (c *Client) RefreshOAuthToken(ctx context.Context, id string, scope config.Scope, providerID string) error {
	rsp, err := c.post(ctx, fmt.Sprintf("/workspaces/%s/config/refresh-oauth", id), nil, jsonBody(struct {
		Scope      config.Scope `json:"scope"`
		ProviderID string       `json:"provider_id"`
	}{Scope: scope, ProviderID: providerID}), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		return fmt.Errorf("failed to refresh OAuth token: %w", err)
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to refresh OAuth token: status code %d", rsp.StatusCode)
	}
	return nil
}

// ProjectNeedsInitialization checks if the project needs
// initialization.
func (c *Client) ProjectNeedsInitialization(ctx context.Context, id string) (bool, error) {
	rsp, err := c.get(ctx, fmt.Sprintf("/workspaces/%s/project/needs-init", id), nil, nil)
	if err != nil {
		return false, fmt.Errorf("failed to check project init: %w", err)
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("failed to check project init: status code %d", rsp.StatusCode)
	}
	var result struct {
		NeedsInit bool `json:"needs_init"`
	}
	if err := json.NewDecoder(rsp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("failed to decode project init response: %w", err)
	}
	return result.NeedsInit, nil
}

// MarkProjectInitialized marks the project as initialized on the
// server.
func (c *Client) MarkProjectInitialized(ctx context.Context, id string) error {
	rsp, err := c.post(ctx, fmt.Sprintf("/workspaces/%s/project/init", id), nil, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to mark project initialized: %w", err)
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to mark project initialized: status code %d", rsp.StatusCode)
	}
	return nil
}

// GetInitializePrompt retrieves the initialization prompt from the
// server.
func (c *Client) GetInitializePrompt(ctx context.Context, id string) (string, error) {
	rsp, err := c.get(ctx, fmt.Sprintf("/workspaces/%s/project/init-prompt", id), nil, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get init prompt: %w", err)
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get init prompt: status code %d", rsp.StatusCode)
	}
	var result struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(rsp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode init prompt response: %w", err)
	}
	return result.Prompt, nil
}
