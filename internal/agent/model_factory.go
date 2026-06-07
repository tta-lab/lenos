package agent

import (
	"context"
	"slices"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/tta-lab/lenos/internal/config"

	"charm.land/fantasy/providers/openrouter"
)

func isExactoSupported(modelID string) bool {
	supportedModels := []string{
		"moonshotai/kimi-k2-0905",
		"deepseek/deepseek-v3.1-terminus",
		"z-ai/glm-4.6",
		"openai/gpt-oss-120b",
		"qwen/qwen3-coder",
	}
	return slices.Contains(supportedModels, modelID)
}

func buildAgentModels(ctx context.Context, isSubAgent bool, cfg *config.ConfigStore) (Model, Model, error) {
	largeModelCfg, ok := cfg.Config().Models[config.SelectedModelTypeLarge]
	if !ok {
		return Model{}, Model{}, errLargeModelNotSelected
	}
	smallModelCfg, ok := cfg.Config().Models[config.SelectedModelTypeSmall]
	if !ok {
		return Model{}, Model{}, errSmallModelNotSelected
	}

	largeProviderCfg, ok := cfg.Config().Providers.Get(largeModelCfg.Provider)
	if !ok {
		return Model{}, Model{}, errLargeModelProviderNotConfigured
	}

	largeProvider, err := buildProvider(largeProviderCfg, largeModelCfg, isSubAgent, cfg)
	if err != nil {
		return Model{}, Model{}, err
	}

	smallProviderCfg, ok := cfg.Config().Providers.Get(smallModelCfg.Provider)
	if !ok {
		return Model{}, Model{}, errSmallModelProviderNotConfigured
	}

	smallProvider, err := buildProvider(smallProviderCfg, smallModelCfg, true, cfg)
	if err != nil {
		return Model{}, Model{}, err
	}

	var largeCatwalkModel *catwalk.Model
	var smallCatwalkModel *catwalk.Model

	for _, m := range largeProviderCfg.Models {
		if m.ID == largeModelCfg.Model {
			largeCatwalkModel = &m
		}
	}
	for _, m := range smallProviderCfg.Models {
		if m.ID == smallModelCfg.Model {
			smallCatwalkModel = &m
		}
	}

	if largeCatwalkModel == nil {
		return Model{}, Model{}, errLargeModelNotFound
	}

	if smallCatwalkModel == nil {
		return Model{}, Model{}, errSmallModelNotFound
	}

	largeModelID := largeModelCfg.Model
	smallModelID := smallModelCfg.Model

	if largeModelCfg.Provider == openrouter.Name && isExactoSupported(largeModelID) {
		largeModelID += ":exacto"
	}

	if smallModelCfg.Provider == openrouter.Name && isExactoSupported(smallModelID) {
		smallModelID += ":exacto"
	}

	largeModel, err := largeProvider.LanguageModel(ctx, largeModelID)
	if err != nil {
		return Model{}, Model{}, err
	}
	smallModel, err := smallProvider.LanguageModel(ctx, smallModelID)
	if err != nil {
		return Model{}, Model{}, err
	}

	return Model{
			Model:      largeModel,
			CatwalkCfg: *largeCatwalkModel,
			ModelCfg:   largeModelCfg,
		}, Model{
			Model:      smallModel,
			CatwalkCfg: *smallCatwalkModel,
			ModelCfg:   smallModelCfg,
		}, nil
}

func buildSelectedModel(ctx context.Context, modelCfg config.SelectedModel, isSubAgent bool, providerNotConfiguredErr, modelNotFoundErr error, cfg *config.ConfigStore) (Model, error) {
	providerCfg, ok := cfg.Config().Providers.Get(modelCfg.Provider)
	if !ok {
		return Model{}, providerNotConfiguredErr
	}

	provider, err := buildProvider(providerCfg, modelCfg, isSubAgent, cfg)
	if err != nil {
		return Model{}, err
	}

	var catwalkModel *catwalk.Model
	for _, m := range providerCfg.Models {
		if m.ID == modelCfg.Model {
			catwalkModel = &m
		}
	}

	if catwalkModel == nil {
		return Model{}, modelNotFoundErr
	}

	modelID := modelCfg.Model
	if modelCfg.Provider == openrouter.Name && isExactoSupported(modelID) {
		modelID += ":exacto"
	}

	languageModel, err := provider.LanguageModel(ctx, modelID)
	if err != nil {
		return Model{}, err
	}

	return Model{
		Model:      languageModel,
		CatwalkCfg: *catwalkModel,
		ModelCfg:   modelCfg,
	}, nil
}
