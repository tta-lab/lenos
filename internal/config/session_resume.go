package config

type SessionResumeDefaults struct {
	AgentName     string
	Provider      string
	Model         string
	ExplicitAgent bool
	ExplicitModel bool
}

func ApplySessionResumeDefaults(store *ConfigStore, defaults SessionResumeDefaults) {
	if store == nil || store.config == nil {
		return
	}
	agentName := defaults.AgentName
	if agentName == "" {
		agentName = AgentCoder
	}
	if !defaults.ExplicitAgent {
		store.Overrides().AgentName = agentName
	}
	if defaults.ExplicitModel || defaults.Provider == "" || defaults.Model == "" {
		return
	}

	tier := SelectedModelTypeLarge
	if agentName == AgentReviewer {
		tier = SelectedModelTypeReview
	}
	store.Overrides().ActiveTier = tier
	store.SetActiveModel(tier, SelectedModel{
		Provider: defaults.Provider,
		Model:    defaults.Model,
	})
}
