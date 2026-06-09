package model

import (
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/ui/common"
	"github.com/tta-lab/lenos/internal/workspace"
)

func selectedAgentModel(com *common.Common) *workspace.AgentModel {
	if com == nil || com.Workspace == nil {
		return nil
	}
	if com.Workspace.AgentIsReady() {
		model := com.Workspace.AgentModel()
		return &model
	}
	cfg := com.Config()
	if cfg == nil {
		return nil
	}
	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return nil
	}
	catwalkModel := cfg.GetModelByType(agentCfg.Model)
	if catwalkModel == nil {
		return nil
	}
	modelCfg, ok := cfg.Models[agentCfg.Model]
	if !ok {
		return nil
	}
	return &workspace.AgentModel{
		CatwalkCfg: *catwalkModel,
		ModelCfg:   modelCfg,
	}
}
