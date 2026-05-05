// Package codex provides the Codex (ChatGPT consumer OAuth) provider definition.
package codex

import (
	_ "embed"
	"encoding/json"
	"log/slog"
	"sync"

	"charm.land/catwalk/pkg/catwalk"
)

//go:embed provider.json
var embedded []byte

const (
	// Name is the provider type/ID for Codex (chatgpt.com backend).
	Name = "codex"

	// BaseURL is the Codex backend base URL. The full Responses path is
	// `<BaseURL>/responses` — fantasy/openai's WithBaseURL appends `/responses`
	// for Responses-API models automatically.
	BaseURL = "https://chatgpt.com/backend-api/codex"

	// AuthURL is the Codex device-auth usercode endpoint.
	AuthURL = "https://auth.openai.com/api/accounts/deviceauth/usercode"

	// TokenURL is the standard OpenAI OAuth token endpoint.
	TokenURL = "https://auth.openai.com/oauth/token"

	// VerifyURL is the URL the user opens to enter the user_code.
	VerifyURL = "https://auth.openai.com/codex/device"

	// ClientID is the OAuth client ID — same one forgecode uses (matches
	// codex-cli's public client_id).
	ClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
)

// Embedded returns the embedded Codex provider definition.
var Embedded = sync.OnceValue(func() catwalk.Provider {
	var provider catwalk.Provider
	if err := json.Unmarshal(embedded, &provider); err != nil {
		slog.Error("Could not parse embedded codex provider data", "err", err)
	}
	return provider
})
