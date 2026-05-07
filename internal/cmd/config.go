package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tta-lab/lenos/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
	Long:  "Manage Lenos configuration, including persistent model selection.",
}

var configSetModelCmd = &cobra.Command{
	Use:   "set-model <large|small> <model>",
	Short: "Set the default model for a tier and persist it",
	Long: `Set the default model for a tier (large or small) and persist it to the
global config file (~/.local/share/lenos/config.json).

The model can be specified as "model-name" or "provider/model-name" to
disambiguate models with the same name across providers.`,
	Example: `
# Set the default large model
lenos config set-model large gpt-4o

# Set the default small model with provider disambiguation
lenos config set-model small openai/gpt-4o-mini

# Set the default large model with provider disambiguation
lenos config set-model large anthropic/claude-3-opus
  `,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		modelTypeStr := strings.ToLower(args[0])
		modelStr := args[1]

		var modelType config.SelectedModelType
		switch modelTypeStr {
		case "large":
			modelType = config.SelectedModelTypeLarge
		case "small":
			modelType = config.SelectedModelTypeSmall
		default:
			return fmt.Errorf("model type must be 'large' or 'small', got %q", modelTypeStr)
		}

		debug, _ := cmd.Flags().GetBool("debug")
		cwd, err := ResolveCwd(cmd)
		if err != nil {
			return err
		}

		store, err := config.Init(cwd, "", debug)
		if err != nil {
			return err
		}

		cfg := store.Config()
		providers := cfg.Providers.Copy()

		providerFilter, modelID := config.ParseModelStrForCLI(providers, modelStr)

		// Validate the provider filter exists.
		if providerFilter != "" {
			if _, ok := providers[providerFilter]; !ok {
				return fmt.Errorf("provider %q not found in configuration. Use 'lenos models' to list available models", providerFilter)
			}
		}

		// Find the model across all enabled providers.
		var matches []config.ModelMatch
		for name, provider := range providers {
			if provider.Disable {
				continue
			}
			for _, m := range provider.Models {
				if strings.EqualFold(m.ID, modelID) &&
					(providerFilter == "" || strings.EqualFold(name, providerFilter)) {
					matches = append(matches, config.ModelMatch{Provider: name, ModelID: m.ID})
				}
			}
		}

		switch {
		case len(matches) == 0:
			return fmt.Errorf("model %q not found. Use 'lenos models' to list available models", modelStr)
		case len(matches) > 1:
			names := make([]string, len(matches))
			for i, m := range matches {
				names[i] = m.Provider
			}
			return fmt.Errorf("model %q found in multiple providers: %s. Please specify provider using 'provider/model' format", modelID, strings.Join(names, ", "))
		}

		match := matches[0]
		selectedModel := config.SelectedModel{
			Provider: match.Provider,
			Model:    match.ModelID,
		}

		slog.Info("Persisting model preference", "type", modelTypeStr, "provider", match.Provider, "model", match.ModelID)
		if err := store.UpdatePreferredModel(config.ScopeGlobal, modelType, selectedModel); err != nil {
			return fmt.Errorf("failed to persist model preference: %w", err)
		}

		fmt.Fprintf(os.Stderr, "Default %s model set to %s/%s\n", modelTypeStr, match.Provider, match.ModelID)
		return nil
	},
}

func init() {
	configCmd.AddCommand(configSetModelCmd)
}
