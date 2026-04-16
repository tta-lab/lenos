package proto

import "github.com/tta-lab/lenos/internal/config"

// ConfigSetRequest represents a request to set a config field.
type ConfigSetRequest struct {
	Scope config.Scope `json:"scope"`
	Key   string       `json:"key"`
	Value any          `json:"value"`
}

// ConfigRemoveRequest represents a request to remove a config field.
type ConfigRemoveRequest struct {
	Scope config.Scope `json:"scope"`
	Key   string       `json:"key"`
}

// ConfigModelRequest represents a request to update the preferred model.
type ConfigModelRequest struct {
	Scope     config.Scope             `json:"scope"`
	ModelType config.SelectedModelType `json:"model_type"`
	Model     config.SelectedModel     `json:"model"`
}

type ConfigProviderKeyRequest struct {
	Scope      config.Scope `json:"scope"`
	ProviderID string       `json:"provider_id"`
	APIKey     any          `json:"api_key"`
}

// ConfigRefreshOAuthRequest represents a request to refresh an OAuth token.
type ConfigRefreshOAuthRequest struct {
	Scope      config.Scope `json:"scope"`
	ProviderID string       `json:"provider_id"`
}

// ImportCopilotResponse represents the response from importing Copilot credentials.
type ImportCopilotResponse struct {
	Token   any  `json:"token"`
	Success bool `json:"success"`
}

// ProjectNeedsInitResponse represents whether a project needs initialization.
type ProjectNeedsInitResponse struct {
	NeedsInit bool `json:"needs_init"`
}

// ProjectInitPromptResponse represents the project initialization prompt.
type ProjectInitPromptResponse struct {
	Prompt string `json:"prompt"`
}

// FileTrackerReadRequest represents a request to record a file read.
type FileTrackerReadRequest struct {
	SessionID string `json:"session_id"`
	Path      string `json:"path"`
}
