package server

import (
	"encoding/json"
	"net/http"

	"github.com/tta-lab/lenos/internal/proto"
)

// handlePostWorkspaceConfigSet sets a configuration field.
//
//	@Summary		Set a config field
//	@Tags			config
//	@Accept			json
//	@Param			id		path	string					true	"Workspace ID"
//	@Param			request	body	proto.ConfigSetRequest	true	"Config set request"
//	@Success		200
//	@Failure		400	{object}	proto.Error
//	@Failure		404	{object}	proto.Error
//	@Failure		500	{object}	proto.Error
//	@Router			/workspaces/{id}/config/set [post]
func (c *controllerV1) handlePostWorkspaceConfigSet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req proto.ConfigSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.server.logError(r, "Failed to decode request", "error", err)
		jsonError(w, http.StatusBadRequest, "failed to decode request")
		return
	}

	if err := c.backend.SetConfigField(id, req.Scope, req.Key, req.Value); err != nil {
		c.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handlePostWorkspaceConfigRemove removes a configuration field.
//
//	@Summary		Remove a config field
//	@Tags			config
//	@Accept			json
//	@Param			id		path	string						true	"Workspace ID"
//	@Param			request	body	proto.ConfigRemoveRequest	true	"Config remove request"
//	@Success		200
//	@Failure		400	{object}	proto.Error
//	@Failure		404	{object}	proto.Error
//	@Failure		500	{object}	proto.Error
//	@Router			/workspaces/{id}/config/remove [post]
func (c *controllerV1) handlePostWorkspaceConfigRemove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req proto.ConfigRemoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.server.logError(r, "Failed to decode request", "error", err)
		jsonError(w, http.StatusBadRequest, "failed to decode request")
		return
	}

	if err := c.backend.RemoveConfigField(id, req.Scope, req.Key); err != nil {
		c.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handlePostWorkspaceConfigModel updates the preferred model.
//
//	@Summary		Set the preferred model
//	@Tags			config
//	@Accept			json
//	@Param			id		path	string						true	"Workspace ID"
//	@Param			request	body	proto.ConfigModelRequest	true	"Config model request"
//	@Success		200
//	@Failure		400	{object}	proto.Error
//	@Failure		404	{object}	proto.Error
//	@Failure		500	{object}	proto.Error
//	@Router			/workspaces/{id}/config/model [post]
func (c *controllerV1) handlePostWorkspaceConfigModel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req proto.ConfigModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.server.logError(r, "Failed to decode request", "error", err)
		jsonError(w, http.StatusBadRequest, "failed to decode request")
		return
	}

	if err := c.backend.UpdatePreferredModel(id, req.Scope, req.ModelType, req.Model); err != nil {
		c.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// @Summary		Set provider API key
// @Tags			config
// @Accept			json
// @Param			id		path	string							true	"Workspace ID"
// @Param			request	body	proto.ConfigProviderKeyRequest	true	"Config provider key request"
// @Success		200
// @Failure		400	{object}	proto.Error
// @Failure		404	{object}	proto.Error
// @Failure		500	{object}	proto.Error
// @Router			/workspaces/{id}/config/provider-key [post]
func (c *controllerV1) handlePostWorkspaceConfigProviderKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req proto.ConfigProviderKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.server.logError(r, "Failed to decode request", "error", err)
		jsonError(w, http.StatusBadRequest, "failed to decode request")
		return
	}

	if err := c.backend.SetProviderAPIKey(id, req.Scope, req.ProviderID, req.APIKey); err != nil {
		c.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handlePostWorkspaceConfigImportCopilot imports Copilot credentials.
//
//	@Summary		Import Copilot credentials
//	@Tags			config
//	@Produce		json
//	@Param			id	path		string						true	"Workspace ID"
//	@Success		200	{object}	proto.ImportCopilotResponse
//	@Failure		404	{object}	proto.Error
//	@Failure		500	{object}	proto.Error
//	@Router			/workspaces/{id}/config/import-copilot [post]
func (c *controllerV1) handlePostWorkspaceConfigImportCopilot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	token, ok, err := c.backend.ImportCopilot(id)
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, proto.ImportCopilotResponse{Token: token, Success: ok})
}

// handlePostWorkspaceConfigRefreshOAuth refreshes an OAuth token for a provider.
//
//	@Summary		Refresh OAuth token
//	@Tags			config
//	@Accept			json
//	@Param			id		path	string							true	"Workspace ID"
//	@Param			request	body	proto.ConfigRefreshOAuthRequest	true	"Refresh OAuth request"
//	@Success		200
//	@Failure		400	{object}	proto.Error
//	@Failure		404	{object}	proto.Error
//	@Failure		500	{object}	proto.Error
//	@Router			/workspaces/{id}/config/refresh-oauth [post]
func (c *controllerV1) handlePostWorkspaceConfigRefreshOAuth(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req proto.ConfigRefreshOAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.server.logError(r, "Failed to decode request", "error", err)
		jsonError(w, http.StatusBadRequest, "failed to decode request")
		return
	}

	if err := c.backend.RefreshOAuthToken(r.Context(), id, req.Scope, req.ProviderID); err != nil {
		c.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleGetWorkspaceProjectNeedsInit reports whether a project needs initialization.
//
//	@Summary		Check if project needs initialization
//	@Tags			project
//	@Produce		json
//	@Param			id	path		string							true	"Workspace ID"
//	@Success		200	{object}	proto.ProjectNeedsInitResponse
//	@Failure		404	{object}	proto.Error
//	@Failure		500	{object}	proto.Error
//	@Router			/workspaces/{id}/project/needs-init [get]
func (c *controllerV1) handleGetWorkspaceProjectNeedsInit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	needs, err := c.backend.ProjectNeedsInitialization(id)
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, proto.ProjectNeedsInitResponse{NeedsInit: needs})
}

// handlePostWorkspaceProjectInit marks the project as initialized.
//
//	@Summary		Mark project as initialized
//	@Tags			project
//	@Param			id	path	string	true	"Workspace ID"
//	@Success		200
//	@Failure		404	{object}	proto.Error
//	@Failure		500	{object}	proto.Error
//	@Router			/workspaces/{id}/project/init [post]
func (c *controllerV1) handlePostWorkspaceProjectInit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := c.backend.MarkProjectInitialized(id); err != nil {
		c.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleGetWorkspaceProjectInitPrompt returns the project initialization prompt.
//
//	@Summary		Get project initialization prompt
//	@Tags			project
//	@Produce		json
//	@Param			id	path		string							true	"Workspace ID"
//	@Success		200	{object}	proto.ProjectInitPromptResponse
//	@Failure		404	{object}	proto.Error
//	@Failure		500	{object}	proto.Error
//	@Router			/workspaces/{id}/project/init-prompt [get]
func (c *controllerV1) handleGetWorkspaceProjectInitPrompt(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	prompt, err := c.backend.InitializePrompt(id)
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, proto.ProjectInitPromptResponse{Prompt: prompt})
}
