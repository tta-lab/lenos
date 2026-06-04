package config

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/csync"
)

func TestParseModelStr(t *testing.T) {
	tests := []struct {
		name            string
		modelStr        string
		expectedFilter  string
		expectedModelID string
		setupProviders  func() map[string]ProviderConfig
	}{
		{
			name:            "simple model with no slashes",
			modelStr:        "gpt-4o",
			expectedFilter:  "",
			expectedModelID: "gpt-4o",
			setupProviders:  setupMockProviders,
		},
		{
			name:            "valid provider and model",
			modelStr:        "openai/gpt-4o",
			expectedFilter:  "openai",
			expectedModelID: "gpt-4o",
			setupProviders:  setupMockProviders,
		},
		{
			name:            "model with multiple slashes and first part is invalid provider",
			modelStr:        "moonshot/kimi-k2",
			expectedFilter:  "",
			expectedModelID: "moonshot/kimi-k2",
			setupProviders:  setupMockProviders,
		},
		{
			name:            "full path with valid provider and model with slashes",
			modelStr:        "synthetic/moonshot/kimi-k2",
			expectedFilter:  "synthetic",
			expectedModelID: "moonshot/kimi-k2",
			setupProviders:  setupMockProvidersWithSlashes,
		},
		{
			name:            "empty model string",
			modelStr:        "",
			expectedFilter:  "",
			expectedModelID: "",
			setupProviders:  setupMockProviders,
		},
		{
			name:            "model with trailing slash but valid provider",
			modelStr:        "openai/",
			expectedFilter:  "openai",
			expectedModelID: "",
			setupProviders:  setupMockProviders,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providers := tt.setupProviders()
			filter, modelID := ParseModelStrForCLI(providers, tt.modelStr)

			require.Equal(t, tt.expectedFilter, filter, "provider filter mismatch")
			require.Equal(t, tt.expectedModelID, modelID, "model ID mismatch")
		})
	}
}

func setupMockProviders() map[string]ProviderConfig {
	return map[string]ProviderConfig{
		"openai": {
			ID:     "openai",
			Name:   "OpenAI",
			Models: []catwalk.Model{{ID: "gpt-4o"}, {ID: "gpt-4o-mini"}},
		},
		"anthropic": {
			ID:     "anthropic",
			Name:   "Anthropic",
			Models: []catwalk.Model{{ID: "claude-3-sonnet"}, {ID: "claude-3-opus"}},
		},
	}
}

func setupMockProvidersWithSlashes() map[string]ProviderConfig {
	return map[string]ProviderConfig{
		"synthetic": {
			ID:   "synthetic",
			Name: "Synthetic",
			Models: []catwalk.Model{
				{ID: "moonshot/kimi-k2"},
				{ID: "deepseek/deepseek-chat"},
			},
		},
		"openai": {
			ID:     "openai",
			Name:   "OpenAI",
			Models: []catwalk.Model{{ID: "gpt-4o"}},
		},
	}
}

func TestFindModels(t *testing.T) {
	tests := []struct {
		name             string
		modelStr         string
		expectedProvider string
		expectedModelID  string
		expectError      bool
		errorContains    string
		setupProviders   func() map[string]ProviderConfig
	}{
		{
			name:             "simple model found in one provider",
			modelStr:         "gpt-4o",
			expectedProvider: "openai",
			expectedModelID:  "gpt-4o",
			expectError:      false,
			setupProviders:   setupMockProviders,
		},
		{
			name:             "model with slashes in ID",
			modelStr:         "moonshot/kimi-k2",
			expectedProvider: "synthetic",
			expectedModelID:  "moonshot/kimi-k2",
			expectError:      false,
			setupProviders:   setupMockProvidersWithSlashes,
		},
		{
			name:             "provider and model with slashes in ID",
			modelStr:         "synthetic/moonshot/kimi-k2",
			expectedProvider: "synthetic",
			expectedModelID:  "moonshot/kimi-k2",
			expectError:      false,
			setupProviders:   setupMockProvidersWithSlashes,
		},
		{
			name:           "model not found",
			modelStr:       "nonexistent-model",
			expectError:    true,
			errorContains:  "not found",
			setupProviders: setupMockProviders,
		},
		{
			name:           "invalid provider specified",
			modelStr:       "nonexistent-provider/gpt-4o",
			expectError:    true,
			errorContains:  "provider",
			setupProviders: setupMockProviders,
		},
		{
			name:          "model found in multiple providers without provider filter",
			modelStr:      "shared-model",
			expectError:   true,
			errorContains: "multiple providers",
			setupProviders: func() map[string]ProviderConfig {
				return map[string]ProviderConfig{
					"openai": {
						ID:     "openai",
						Models: []catwalk.Model{{ID: "shared-model"}},
					},
					"anthropic": {
						ID:     "anthropic",
						Models: []catwalk.Model{{ID: "shared-model"}},
					},
				}
			},
		},
		{
			name:           "empty model string",
			modelStr:       "",
			expectError:    true,
			errorContains:  "not found",
			setupProviders: setupMockProviders,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providers := tt.setupProviders()

			// Use findModels with the model as "large" and empty "small".
			matches, err := findModels(providers, tt.modelStr)
			if err != nil {
				if tt.expectError {
					require.Contains(t, err.Error(), tt.errorContains)
				} else {
					require.NoError(t, err)
				}
				return
			}

			// Validate the matches.
			match, err := validateMatches(matches, tt.modelStr, "large")

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errorContains)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedProvider, match.Provider)
				require.Equal(t, tt.expectedModelID, match.ModelID)
			}
		})
	}
}

func TestApplyEphemeralModelOverride(t *testing.T) {
	t.Parallel()

	providerSet := map[string]ProviderConfig{
		"openai": {
			ID:     "openai",
			Models: []catwalk.Model{{ID: "gpt-4o"}, {ID: "shared-model"}},
		},
		"anthropic": {
			ID:     "anthropic",
			Models: []catwalk.Model{{ID: "claude"}, {ID: "shared-model"}},
		},
	}

	cases := []struct {
		name            string
		modelOverride   string
		useSmallTier    bool
		defaultTier     SelectedModelType
		reasoningEffort string
		wantTier        SelectedModelType
		wantModel       *SelectedModel
		wantErr         string
	}{
		{name: "no-op", modelOverride: "", useSmallTier: false, wantTier: SelectedModelTypeLarge, wantModel: nil, wantErr: ""},
		{name: "small-tier-only", modelOverride: "", useSmallTier: true, wantTier: SelectedModelTypeSmall, wantModel: nil, wantErr: ""},
		{name: "review-tier-only", modelOverride: "", useSmallTier: false, defaultTier: SelectedModelTypeReview, wantTier: SelectedModelTypeReview, wantModel: nil, wantErr: ""},
		{name: "review-reasoning-effort-without-model", modelOverride: "", useSmallTier: false, defaultTier: SelectedModelTypeReview, reasoningEffort: "high", wantTier: SelectedModelTypeReview, wantModel: nil, wantErr: ""},
		{name: "large-reasoning-effort", modelOverride: "gpt-4o", useSmallTier: false, reasoningEffort: "high", wantTier: SelectedModelTypeLarge, wantModel: &SelectedModel{Provider: "openai", Model: "gpt-4o", ReasoningEffort: "high"}, wantErr: ""},
		{name: "invalid-reasoning-effort", modelOverride: "", useSmallTier: false, reasoningEffort: "low", wantTier: SelectedModelTypeLarge, wantModel: nil, wantErr: "invalid reasoning effort"},
		{name: "large-override-name-only", modelOverride: "gpt-4o", useSmallTier: false, wantTier: SelectedModelTypeLarge, wantModel: &SelectedModel{Provider: "openai", Model: "gpt-4o"}, wantErr: ""},
		{name: "small-override-with-flag", modelOverride: "gpt-4o", useSmallTier: true, wantTier: SelectedModelTypeSmall, wantModel: &SelectedModel{Provider: "openai", Model: "gpt-4o"}, wantErr: ""},
		{name: "provider-prefixed", modelOverride: "openai/gpt-4o", useSmallTier: false, wantTier: SelectedModelTypeLarge, wantModel: &SelectedModel{Provider: "openai", Model: "gpt-4o"}, wantErr: ""},
		{name: "case-insensitive", modelOverride: "GPT-4O", useSmallTier: false, wantTier: SelectedModelTypeLarge, wantModel: &SelectedModel{Provider: "openai", Model: "gpt-4o"}, wantErr: ""},
		{name: "ambiguous", modelOverride: "shared-model", useSmallTier: false, wantTier: SelectedModelTypeLarge, wantModel: nil, wantErr: "multiple providers"},
		{name: "not-found", modelOverride: "nonexistent", useSmallTier: false, wantTier: SelectedModelTypeLarge, wantModel: nil, wantErr: "not found"},
		{name: "unknown-provider", modelOverride: "bogus/gpt-4o", useSmallTier: false, wantTier: SelectedModelTypeLarge, wantModel: nil, wantErr: "not found"},
		{name: "large-flag-with-override", modelOverride: "claude", useSmallTier: false, wantTier: SelectedModelTypeLarge, wantModel: &SelectedModel{Provider: "anthropic", Model: "claude"}, wantErr: ""},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &ConfigStore{
				overrides: RuntimeOverrides{},
				config: &Config{
					Models:    make(map[SelectedModelType]SelectedModel),
					Providers: csync.NewMapFrom(providerSet),
				},
			}

			err := ApplyEphemeralModelOverride(store, tt.modelOverride, tt.useSmallTier, tt.defaultTier, tt.reasoningEffort)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)

			require.Equal(t, tt.wantTier, store.Overrides().ActiveTier, "ActiveTier mismatch")

			if tt.wantModel == nil {
				_, exists := store.config.Models[tt.wantTier]
				require.False(t, exists, "expected no model mutation for tier %s", tt.wantTier)
			} else {
				got, exists := store.config.Models[tt.wantTier]
				require.True(t, exists, "expected model entry for tier %s", tt.wantTier)
				require.Equal(t, *tt.wantModel, got)
			}
		})
	}
}
