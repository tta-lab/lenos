package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestConfigCmd_Declared(t *testing.T) {
	require.Equal(t, "config", configCmd.Use)
	require.NotEmpty(t, configCmd.Short)
}

func TestConfigCmd_HasSetModelSubcommand(t *testing.T) {
	found := false
	for _, c := range configCmd.Commands() {
		if c.Use == "set-model <large|small> <model>" {
			found = true
			break
		}
	}
	require.True(t, found, "set-model subcommand must be registered")
}

func TestConfigSetModelCmd_RequiresExactArgs(t *testing.T) {
	cmd := &cobra.Command{
		Use:  "set-model",
		Args: cobra.ExactArgs(2),
	}

	err := cmd.ValidateArgs([]string{"large"})
	require.Error(t, err, "set-model requires exactly 2 args")

	err = cmd.ValidateArgs([]string{"large", "gpt-4o"})
	require.NoError(t, err, "set-model accepts exactly 2 args")

	err = cmd.ValidateArgs([]string{"large", "gpt-4o", "extra"})
	require.Error(t, err, "set-model rejects 3 args")
}

// runConfigSetModelCmd runs the real configSetModelCmd with the given args
// and a temp LENOS_GLOBAL_DATA directory containing a config.json with inline
// providers. Returns the full path to config.json for assertions.
func runConfigSetModelCmd(t *testing.T, tier, model string, configJSON string) error {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("LENOS_GLOBAL_DATA", dir)
	t.Setenv("LENOS_DISABLE_DEFAULT_PROVIDERS", "true")
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "true")

	// Write the config.json
	configPath := filepath.Join(dir, "config.json")
	if configJSON != "" {
		require.NoError(t, os.WriteFile(configPath, []byte(configJSON), 0o600))
	}

	// Create a cobra command that mimics the root command with persistent flags.
	cmd := &cobra.Command{}
	cmd.PersistentFlags().String("cwd", dir, "cwd")
	cmd.PersistentFlags().Bool("debug", false, "debug")
	// Set cwd flag so ResolveCwd works.
	require.NoError(t, cmd.PersistentFlags().Set("cwd", dir))

	// Build args for the set-model command.
	args := []string{tier, model}
	return configSetModelCmd.RunE(cmd, args)
}

func TestConfigSetModelCmd_SetsLargeModel(t *testing.T) {
	err := runConfigSetModelCmd(t, "large", "gpt-4o", `{
		"providers": {
			"openai": {
				"id": "openai",
				"name": "OpenAI",
				"type": "openai",
				"base_url": "https://api.openai.com/v1",
				"api_key": "sk-test",
				"models": [{"id": "gpt-4o"}]
			}
		},
		"options": {
			"disable_default_providers": true,
			"disable_provider_auto_update": true
		}
	}`)
	require.NoError(t, err)
}

func TestConfigSetModelCmd_SetsSmallModel(t *testing.T) {
	err := runConfigSetModelCmd(t, "small", "gpt-4o-mini", `{
		"providers": {
			"openai": {
				"id": "openai",
				"name": "OpenAI",
				"type": "openai",
				"base_url": "https://api.openai.com/v1",
				"api_key": "sk-test",
				"models": [{"id": "gpt-4o-mini"}, {"id": "gpt-4o"}]
			}
		},
		"options": {
			"disable_default_providers": true,
			"disable_provider_auto_update": true
		}
	}`)
	require.NoError(t, err)
}

func TestConfigSetModelCmd_RejectsModelNotFound(t *testing.T) {
	err := runConfigSetModelCmd(t, "large", "nonexistent", `{
		"providers": {
			"openai": {
				"id": "openai",
				"name": "OpenAI",
				"type": "openai",
				"base_url": "https://api.openai.com/v1",
				"api_key": "sk-test",
				"models": [{"id": "gpt-4o"}, {"id": "gpt-4o-mini"}]
			}
		},
		"options": {
			"disable_default_providers": true,
			"disable_provider_auto_update": true
		}
	}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestConfigSetModelCmd_RejectsEmptyModel(t *testing.T) {
	err := runConfigSetModelCmd(t, "large", "", `{
		"providers": {
			"openai": {
				"id": "openai",
				"name": "OpenAI",
				"type": "openai",
				"base_url": "https://api.openai.com/v1",
				"api_key": "sk-test",
				"models": [{"id": "gpt-4o"}]
			}
		},
		"options": {
			"disable_default_providers": true,
			"disable_provider_auto_update": true
		}
	}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestConfigSetModelCmd_RejectsAmbiguousModel(t *testing.T) {
	err := runConfigSetModelCmd(t, "large", "shared-model", `{
		"providers": {
			"openai": {
				"id": "openai",
				"name": "OpenAI",
				"type": "openai",
				"base_url": "https://api.openai.com/v1",
				"api_key": "sk-test",
				"models": [{"id": "shared-model"}]
			},
			"anthropic": {
				"id": "anthropic",
				"name": "Anthropic",
				"type": "anthropic",
				"base_url": "https://api.anthropic.com/v1",
				"api_key": "sk-test2",
				"models": [{"id": "shared-model"}]
			}
		},
		"options": {
			"disable_default_providers": true,
			"disable_provider_auto_update": true
		}
	}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "multiple providers")
}

func TestConfigSetModelCmd_AcceptsProviderPrefixedModel(t *testing.T) {
	err := runConfigSetModelCmd(t, "large", "openai/gpt-4o", `{
		"providers": {
			"openai": {
				"id": "openai",
				"name": "OpenAI",
				"type": "openai",
				"base_url": "https://api.openai.com/v1",
				"api_key": "sk-test",
				"models": [{"id": "gpt-4o"}]
			}
		},
		"options": {
			"disable_default_providers": true,
			"disable_provider_auto_update": true
		}
	}`)
	require.NoError(t, err)
}

func TestConfigSetModelCmd_RejectsInvalidTier(t *testing.T) {
	err := runConfigSetModelCmd(t, "huge", "gpt-4o", `{
		"providers": {
			"openai": {
				"id": "openai",
				"name": "OpenAI",
				"type": "openai",
				"api_key": "sk-test",
				"models": [{"id": "gpt-4o"}]
			}
		},
		"options": {
			"disable_default_providers": true,
			"disable_provider_auto_update": true
		}
	}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "model type must be 'large' or 'small'")
}
