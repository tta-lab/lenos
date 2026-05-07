package config

import (
	"fmt"
	"log/slog"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	xstrings "github.com/charmbracelet/x/exp/strings"
)

// ApplyEphemeralModelOverride applies model overrides from CLI flags to the
// in-memory ConfigStore. It sets RuntimeOverrides.ActiveTier based on
// useSmallTier and optionally overrides the active tier's model.
// If modelOverride is empty, only the active tier is set (no model mutation).
// This is the single override path for both `lenos` and `lenos run`.
func ApplyEphemeralModelOverride(store *ConfigStore, modelOverride string, useSmallTier bool) error {
	activeTier := SelectedModelTypeLarge
	if useSmallTier {
		activeTier = SelectedModelTypeSmall
	}
	store.Overrides().ActiveTier = activeTier

	if modelOverride == "" {
		return nil
	}

	providers := store.config.Providers.Copy()
	matches, err := findModels(providers, modelOverride)
	if err != nil {
		return err
	}
	found, err := validateMatches(matches, modelOverride, string(activeTier))
	if err != nil {
		return err
	}

	slog.Info("Overriding model for session", "tier", activeTier, "provider", found.Provider, "model", found.ModelID)
	store.config.Models[activeTier] = SelectedModel{
		Provider: found.Provider,
		Model:    found.ModelID,
	}
	return nil
}

// GetDefaultSmallModel returns the default small model for the given
// provider. Falls back to the large model if no default is found.
// Retained for legacy callers; new code should use ApplyEphemeralModelOverride.
func (s *ConfigStore) GetDefaultSmallModel(providerID string) SelectedModel {
	cfg := s.config
	largeModelCfg := cfg.Models[SelectedModelTypeLarge]

	knownProviders, _ := Providers(cfg)
	var knownProvider *catwalk.Provider
	for _, p := range knownProviders {
		if string(p.ID) == providerID {
			knownProvider = &p
			break
		}
	}

	if knownProvider == nil {
		slog.Warn("Using large model as small model for unknown provider", "provider", providerID, "model", largeModelCfg.Model)
		return largeModelCfg
	}

	defaultSmallModelID := knownProvider.DefaultSmallModelID
	model := cfg.GetModel(providerID, defaultSmallModelID)
	if model == nil {
		slog.Warn("Default small model not found, using large model", "provider", providerID, "model", largeModelCfg.Model)
		return largeModelCfg
	}

	slog.Info("Using provider default small model", "provider", providerID, "model", defaultSmallModelID)
	return SelectedModel{
		Provider:        providerID,
		Model:           defaultSmallModelID,
		MaxTokens:       model.DefaultMaxTokens,
		ReasoningEffort: model.DefaultReasoningEffort,
	}
}

// ParseModelStrForCLI parses a model string into provider filter and model ID.
func ParseModelStrForCLI(providers map[string]ProviderConfig, modelStr string) (providerFilter, modelID string) {
	parts := strings.Split(modelStr, "/")
	if len(parts) == 1 {
		return "", parts[0]
	}
	if _, ok := providers[parts[0]]; ok {
		return parts[0], strings.Join(parts[1:], "/")
	}
	return "", modelStr
}

// ModelMatch represents a found model.
type ModelMatch struct {
	Provider string
	ModelID  string
}

// findModels searches for a single model string across all providers.
func findModels(providers map[string]ProviderConfig, modelStr string) ([]ModelMatch, error) {
	providerFilter, modelID := ParseModelStrForCLI(providers, modelStr)

	if providerFilter != "" {
		if _, ok := providers[providerFilter]; !ok {
			return nil, fmt.Errorf("model: provider %q not found in configuration. Use 'lenos models' to list available models", providerFilter)
		}
	}

	var matches []ModelMatch
	for name, provider := range providers {
		if provider.Disable {
			continue
		}
		for _, m := range provider.Models {
			if filter(modelID, providerFilter, m.ID, name) {
				matches = append(matches, ModelMatch{Provider: name, ModelID: m.ID})
			}
		}
	}
	return matches, nil
}

func filter(modelFilter, providerFilter, model, provider string) bool {
	return modelFilter != "" && strings.EqualFold(model, modelFilter) &&
		(providerFilter == "" || strings.EqualFold(provider, providerFilter))
}

// Validate and return a single match.
func validateMatches(matches []ModelMatch, modelID, label string) (ModelMatch, error) {
	switch {
	case len(matches) == 0:
		return ModelMatch{}, fmt.Errorf("%s model %q not found", label, modelID)
	case len(matches) > 1:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Provider
		}
		return ModelMatch{}, fmt.Errorf(
			"%s model: model %q found in multiple providers: %s. Please specify provider using 'provider/model' format",
			label,
			modelID,
			xstrings.EnglishJoin(names, true),
		)
	}
	return matches[0], nil
}
