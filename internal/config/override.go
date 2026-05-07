package config

import (
	"fmt"
	"log/slog"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	xstrings "github.com/charmbracelet/x/exp/strings"
)

// ApplyEphemeralModelOverride applies model overrides from CLI flags to the
// in-memory ConfigStore. It writes directly to store.config.Models without
// persisting to disk. This is the single override path for both `lenos` and
// `lenos run`.
//
// Conflict resolution: if both largeModel and smallModel are provided, both
// are set. If only largeModel is provided, smallModel defaults to the
// provider's default small model (via GetDefaultSmallModel).
// If neither is provided, this is a no-op.
//
// largeModel and smallModel accept "model-name" or "provider/model-name" format.
func ApplyEphemeralModelOverride(store *ConfigStore, largeModel, smallModel string) error {
	if largeModel == "" && smallModel == "" {
		return nil
	}

	providers := store.config.Providers.Copy()

	largeMatches, smallMatches, err := findModels(providers, largeModel, smallModel)
	if err != nil {
		return err
	}

	var largeProviderID string

	// Override large model.
	if largeModel != "" {
		found, err := validateMatches(largeMatches, largeModel, "large")
		if err != nil {
			return err
		}
		largeProviderID = found.provider
		slog.Info("Overriding large model for session", "provider", found.provider, "model", found.modelID)
		store.config.Models[SelectedModelTypeLarge] = SelectedModel{
			Provider: found.provider,
			Model:    found.modelID,
		}
	}

	// Override small model.
	switch {
	case smallModel != "":
		found, err := validateMatches(smallMatches, smallModel, "small")
		if err != nil {
			return err
		}
		slog.Info("Overriding small model for session", "provider", found.provider, "model", found.modelID)
		store.config.Models[SelectedModelTypeSmall] = SelectedModel{
			Provider: found.provider,
			Model:    found.modelID,
		}

	case largeModel != "":
		// No small model specified, but large model was — use provider's default.
		smallCfg := store.GetDefaultSmallModel(largeProviderID)
		store.config.Models[SelectedModelTypeSmall] = smallCfg
	}

	return nil
}

// GetDefaultSmallModel returns the default small model for the given
// provider. Falls back to the large model if no default is found.
func (s *ConfigStore) GetDefaultSmallModel(providerID string) SelectedModel {
	cfg := s.config
	largeModelCfg := cfg.Models[SelectedModelTypeLarge]

	// Find the provider in the known providers list to get its default small model.
	knownProviders, _ := Providers(cfg)
	var knownProvider *catwalk.Provider
	for _, p := range knownProviders {
		if string(p.ID) == providerID {
			knownProvider = &p
			break
		}
	}

	// For unknown/local providers, use the large model as small.
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

// parseModelStr parses a model string into provider filter and model ID.
// Format: "model-name" or "provider/model-name" or "synthetic/moonshot/kimi-k2".
// This function only checks if the first component is a valid provider name; if not,
// it treats the entire string as a model ID (which may contain slashes).
func parseModelStr(providers map[string]ProviderConfig, modelStr string) (providerFilter, modelID string) {
	parts := strings.Split(modelStr, "/")
	if len(parts) == 1 {
		return "", parts[0]
	}
	// Check if the first part is a valid provider name
	if _, ok := providers[parts[0]]; ok {
		return parts[0], strings.Join(parts[1:], "/")
	}

	// First part is not a valid provider, treat entire string as model ID
	return "", modelStr
}

// modelMatch represents a found model.
type modelMatch struct {
	provider string
	modelID  string
}

func findModels(providers map[string]ProviderConfig, largeModel, smallModel string) ([]modelMatch, []modelMatch, error) {
	largeProviderFilter, largeModelID := parseModelStr(providers, largeModel)
	smallProviderFilter, smallModelID := parseModelStr(providers, smallModel)

	// Validate provider filters exist.
	for _, pf := range []struct {
		filter, label string
	}{
		{largeProviderFilter, "large"},
		{smallProviderFilter, "small"},
	} {
		if pf.filter != "" {
			if _, ok := providers[pf.filter]; !ok {
				return nil, nil, fmt.Errorf("%s model: provider %q not found in configuration. Use 'lenos models' to list available models", pf.label, pf.filter)
			}
		}
	}

	// Find matching models in a single pass.
	var largeMatches, smallMatches []modelMatch
	for name, provider := range providers {
		if provider.Disable {
			continue
		}
		for _, m := range provider.Models {
			if filter(largeModelID, largeProviderFilter, m.ID, name) {
				largeMatches = append(largeMatches, modelMatch{provider: name, modelID: m.ID})
			}
			if filter(smallModelID, smallProviderFilter, m.ID, name) {
				smallMatches = append(smallMatches, modelMatch{provider: name, modelID: m.ID})
			}
		}
	}

	return largeMatches, smallMatches, nil
}

func filter(modelFilter, providerFilter, model, provider string) bool {
	return modelFilter != "" && strings.EqualFold(model, modelFilter) &&
		(providerFilter == "" || strings.EqualFold(provider, providerFilter))
}

// Validate and return a single match.
func validateMatches(matches []modelMatch, modelID, label string) (modelMatch, error) {
	switch {
	case len(matches) == 0:
		return modelMatch{}, fmt.Errorf("%s model %q not found", label, modelID)
	case len(matches) > 1:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.provider
		}
		return modelMatch{}, fmt.Errorf(
			"%s model: model %q found in multiple providers: %s. Please specify provider using 'provider/model' format",
			label,
			modelID,
			xstrings.EnglishJoin(names, true),
		)
	}
	return matches[0], nil
}
