package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"charm.land/fantasy"
	"github.com/tta-lab/lenos/internal/config"
)

// isUnauthorized reports whether err is a fantasy.ProviderError with a 401 status
// code, signalling that OAuth/API-key credentials have expired.
func isUnauthorized(err error) bool {
	var providerErr *fantasy.ProviderError
	return errors.As(err, &providerErr) && providerErr.StatusCode == http.StatusUnauthorized
}

func refreshOAuth2Token(ctx context.Context, providerCfg config.ProviderConfig, cfg *config.ConfigStore) error {
	if err := cfg.RefreshOAuthToken(ctx, config.ScopeGlobal, providerCfg.ID); err != nil {
		slog.Error("Failed to refresh OAuth token", "provider", providerCfg.ID, "error", err)
		return err
	}
	return nil
}

func refreshApiKeyTemplate(ctx context.Context, providerCfg config.ProviderConfig, cfg *config.ConfigStore) error {
	newAPIKey, err := cfg.Resolve(providerCfg.APIKeyTemplate)
	if err != nil {
		slog.Error("Failed to resolve API key template", "provider", providerCfg.ID, "error", err)
		return err
	}
	providerCfg.APIKey = newAPIKey
	cfg.Config().Providers.Set(providerCfg.ID, providerCfg)
	return nil
}

// maybeRefreshToken refreshes the OAuth token if expired, rebuilds the
// provider, and updates the local model/providerCfg references. Returns nil
// if no refresh was needed.
// Ported from upstream 8cd4786c (extract token refresh helpers).
func maybeRefreshToken(
	ctx context.Context,
	model *Model,
	providerCfg *config.ProviderConfig,
	cfg *config.ConfigStore,
	updateModels func(context.Context) error,
	getModel func() Model,
) error {
	if providerCfg.OAuthToken == nil || !providerCfg.OAuthToken.IsExpired() {
		return nil
	}
	slog.Debug("OAuth token expired, refreshing", "provider", providerCfg.ID)
	if err := refreshOAuth2Token(ctx, *providerCfg, cfg); err != nil {
		return err
	}
	if err := updateModels(ctx); err != nil {
		return fmt.Errorf("rebuild model after token refresh: %w", err)
	}
	*model = getModel()
	freshCfg, ok := cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return fmt.Errorf("provider %s not found after token refresh", model.ModelCfg.Provider)
	}
	*providerCfg = freshCfg
	return nil
}
