package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplySessionResumeDefaultsRestoresAgentAndModel(t *testing.T) {
	store := &ConfigStore{
		config: &Config{
			Models: map[SelectedModelType]SelectedModel{
				SelectedModelTypeLarge: {
					Provider: "openai",
					Model:    "gpt-4o",
				},
			},
		},
	}

	ApplySessionResumeDefaults(store, SessionResumeDefaults{
		AgentName: "reviewer",
		Provider:  "anthropic",
		Model:     "claude-sonnet-4",
	})

	require.Equal(t, AgentReviewer, store.Overrides().AgentName)
	require.Equal(t, SelectedModelTypeReview, store.Overrides().ActiveTier)
	require.Equal(t, SelectedModel{
		Provider: "anthropic",
		Model:    "claude-sonnet-4",
	}, store.Config().Models[SelectedModelTypeReview])
}

func TestApplySessionResumeDefaultsKeepsExplicitOverrides(t *testing.T) {
	store := &ConfigStore{
		config: &Config{
			Models: map[SelectedModelType]SelectedModel{
				SelectedModelTypeLarge: {
					Provider: "openai",
					Model:    "gpt-4o",
				},
				SelectedModelTypeSmall: {
					Provider: "openai",
					Model:    "gpt-4o-mini",
				},
			},
		},
		overrides: RuntimeOverrides{
			AgentName:  AgentCoder,
			ActiveTier: SelectedModelTypeSmall,
		},
	}

	ApplySessionResumeDefaults(store, SessionResumeDefaults{
		AgentName:     "reviewer",
		Provider:      "anthropic",
		Model:         "claude-sonnet-4",
		ExplicitAgent: true,
		ExplicitModel: true,
	})

	require.Equal(t, AgentCoder, store.Overrides().AgentName)
	require.Equal(t, SelectedModelTypeSmall, store.Overrides().ActiveTier)
	require.Equal(t, SelectedModel{
		Provider: "openai",
		Model:    "gpt-4o-mini",
	}, store.Config().Models[SelectedModelTypeSmall])
}
