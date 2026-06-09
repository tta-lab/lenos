package atif

type Trajectory struct {
	SchemaVersion        string                  `json:"schema_version"`
	TrajectoryID         string                  `json:"trajectory_id,omitempty"`
	SessionID            string                  `json:"session_id,omitempty"`
	Agent                Agent                   `json:"agent,omitempty"`
	Steps                []Step                  `json:"steps,omitempty"`
	FinalMetrics         FinalMetrics            `json:"final_metrics,omitempty"`
	SubagentTrajectories []SubagentTrajectoryRef `json:"subagent_trajectories,omitempty"`
	Extra                map[string]any          `json:"extra,omitempty"`
}

type Agent struct {
	Name            string         `json:"name,omitempty"`
	Version         string         `json:"version,omitempty"`
	ModelName       string         `json:"model_name,omitempty"`
	ToolDefinitions []ToolCall     `json:"tool_definitions,omitempty"`
	Extra           map[string]any `json:"extra,omitempty"`
}

type Step struct {
	Source           string         `json:"source,omitempty"`
	Message          string         `json:"message,omitempty"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ModelName        string         `json:"model_name,omitempty"`
	LLMCallCount     int            `json:"llm_call_count,omitempty"`
	Observation      *Observation   `json:"observation,omitempty"`
	Metrics          *Metrics       `json:"metrics,omitempty"`
	Extra            map[string]any `json:"extra,omitempty"`
}

type Observation struct {
	Results []ObservationResult `json:"results,omitempty"`
	Extra   map[string]any      `json:"extra,omitempty"`
}

type ObservationResult struct {
	Content      string         `json:"content,omitempty"`
	SourceCallID string         `json:"source_call_id,omitempty"`
	Extra        map[string]any `json:"extra,omitempty"`
}

type Metrics struct {
	PromptTokens     int64          `json:"prompt_tokens,omitempty"`
	CompletionTokens int64          `json:"completion_tokens,omitempty"`
	CachedTokens     int64          `json:"cached_tokens,omitempty"`
	CostUSD          *float64       `json:"cost_usd,omitempty"`
	Extra            map[string]any `json:"extra,omitempty"`
}

type FinalMetrics struct {
	TotalPromptTokens     int64          `json:"total_prompt_tokens,omitempty"`
	TotalCompletionTokens int64          `json:"total_completion_tokens,omitempty"`
	TotalCachedTokens     int64          `json:"total_cached_tokens,omitempty"`
	TotalCostUSD          *float64       `json:"total_cost_usd,omitempty"`
	TotalSteps            int            `json:"total_steps,omitempty"`
	Extra                 map[string]any `json:"extra,omitempty"`
}

type ToolCall struct {
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
	Extra map[string]any `json:"extra,omitempty"`
}

type SubagentTrajectoryRef struct {
	TrajectoryID string         `json:"trajectory_id,omitempty"`
	Path         string         `json:"path,omitempty"`
	Extra        map[string]any `json:"extra,omitempty"`
}
