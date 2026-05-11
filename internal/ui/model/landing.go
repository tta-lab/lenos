package model

import (
	"charm.land/lipgloss/v2"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/ui/common"
	"github.com/tta-lab/lenos/internal/workspace"
)

// selectedAgentModel returns the active agent model. It falls back to the
// configured coder model only before the agent coordinator is ready.
func (m *UI) selectedAgentModel() *workspace.AgentModel {
	return selectedAgentModel(m.com)
}

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

// landingView renders the landing page view showing the current working
// directory and model information.
func (m *UI) landingView() string {
	t := m.com.Styles
	width := m.layout.main.Dx()
	cwd := common.PrettyPath(t, m.com.Workspace.WorkingDir(), width)

	parts := []string{
		cwd,
	}

	parts = append(parts, "", m.modelInfo(width))
	infoSection := lipgloss.JoinVertical(lipgloss.Left, parts...)

	return lipgloss.NewStyle().
		Width(width).
		Height(m.layout.main.Dy() - 1).
		PaddingTop(1).
		Render(
			lipgloss.JoinVertical(lipgloss.Left, infoSection, ""),
		)
}
