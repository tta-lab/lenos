package app

import (
	"fmt"
	"strings"

	xstrings "github.com/charmbracelet/x/exp/strings"
	"github.com/tta-lab/lenos/internal/config"
)

// parseModelStr parses a model string into provider filter and model ID.
// Format: "model-name" or "provider/model-name" or "synthetic/moonshot/kimi-k2".
// This function only checks if the first component is a valid provider name; if not,
// it treats the entire string as a model ID (which may contain slashes).
func parseModelStr(providers map[string]config.ProviderConfig, modelStr string) (providerFilter, modelID string) {
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

func findModels(providers map[string]config.ProviderConfig, modelStr string) ([]modelMatch, error) {
	providerFilter, modelID := parseModelStr(providers, modelStr)

	if providerFilter != "" {
		if _, ok := providers[providerFilter]; !ok {
			return nil, fmt.Errorf("model: provider %q not found in configuration. Use 'lenos models' to list available models", providerFilter)
		}
	}

	var matches []modelMatch
	for name, provider := range providers {
		if provider.Disable {
			continue
		}
		for _, m := range provider.Models {
			if filter(modelID, providerFilter, m.ID, name) {
				matches = append(matches, modelMatch{provider: name, modelID: m.ID})
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
