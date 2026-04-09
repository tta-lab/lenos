package proto

import (
	"charm.land/catwalk/pkg/catwalk"
	"github.com/tta-lab/lenos/internal/config"
)

// Workspace represents a running app.App workspace with its associated
// resources and state.
type Workspace struct {
	ID      string         `json:"id"`
	Path    string         `json:"path"`
	Debug   bool           `json:"debug,omitempty"`
	DataDir string         `json:"data_dir,omitempty"`
	Version string         `json:"version,omitempty"`
	Config  *config.Config `json:"config,omitempty"`
	Env     []string       `json:"env,omitempty"`
}

// Error represents an error response.
type Error struct {
	Message string `json:"message"`
}

// AgentInfo represents information about the agent.
type AgentInfo struct {
	IsBusy   bool                 `json:"is_busy"`
	IsReady  bool                 `json:"is_ready"`
	Model    catwalk.Model        `json:"model"`
	ModelCfg config.SelectedModel `json:"model_cfg"`
}

// IsZero checks if the AgentInfo is zero-valued.
func (a AgentInfo) IsZero() bool {
	return !a.IsBusy && !a.IsReady && a.Model.ID == ""
}

// AgentMessage represents a message sent to the agent.
type AgentMessage struct {
	SessionID   string       `json:"session_id"`
	Prompt      string       `json:"prompt"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// AgentSession represents a session with its busy status.
type AgentSession struct {
	Session
	IsBusy bool `json:"is_busy"`
}

// IsZero checks if the AgentSession is zero-valued.
func (a AgentSession) IsZero() bool {
	return a == AgentSession{}
}
