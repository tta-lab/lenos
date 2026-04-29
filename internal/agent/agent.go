// Package agent is the core orchestration layer for Lenos AI agents.
//
// It provides session-based AI agent functionality for managing
// conversations, tool execution, and message handling. It coordinates
// interactions between language models, messages, sessions, and tools while
// handling features like automatic summarization, queuing, and token
// management.
package agent

import (
	"context"
	_ "embed"
	"fmt"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/tta-lab/temenos/client"

	"github.com/tta-lab/lenos/internal/agent/notify"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/csync"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/pubsub"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/transcript"
	"github.com/tta-lab/lenos/internal/version"
)

const (
	DefaultSessionName = "Untitled Session"

	// Constants for auto-summarization thresholds
	largeContextWindowThreshold = 200_000
	largeContextWindowBuffer    = 20_000
	smallContextWindowRatio     = 0.2
)

var userAgent = fmt.Sprintf("Lenos/%s (https://github.com/tta-lab/lenos)", version.Version)

//go:embed templates/summary.md
var summaryPrompt []byte

// SessionAgentCall carries one user-initiated turn through the agent loop.
// LogosCfg is gone in the bash-first rewrite; the loop now needs the runtime
// pieces it used to derive from logos.Config directly.
type SessionAgentCall struct {
	SessionID string
	Prompt    string

	// ProviderID is the config-side provider identifier (e.g.
	// "minimax-china", "openrouter"), NOT the fantasy protocol name (e.g.
	// "anthropic"). This is what the UI looks up via cfg.GetModel; storing
	// the fantasy Provider.Name() here was a regression that caused
	// "Unknown Model" in the footer.
	ProviderID string

	// ProviderOptions are the per-provider streaming options merged from
	// catwalk + provider config + model config (anthropic thinking, openai
	// reasoning_effort, etc).
	ProviderOptions fantasy.ProviderOptions

	// Sandbox controls runner selection. When true and SandboxClient is set
	// the loop runs each emit through temenos; otherwise it falls back to
	// LocalRunner with a clear warning.
	Sandbox       bool
	SandboxClient *client.Client

	// Env is the explicit environment overlay for each subprocess. The loop
	// sets LENOS_SESSION_ID / LENOS_DATA_DIR on top of this so log CLI in
	// Phase 3 can resolve the .md path.
	Env map[string]string

	// AllowedPaths is the read/write bound for the runner. The first entry
	// also becomes the subprocess working directory.
	AllowedPaths []client.AllowedPath
}

type SessionAgent interface {
	Run(context.Context, SessionAgentCall) error
	SetModels(large Model, small Model)
	SetTools(tools []fantasy.AgentTool)
	SetSystemPrompt(systemPrompt string)
	Cancel(sessionID string)
	CancelAll()
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	QueuedPrompts(sessionID string) int
	QueuedPromptsList(sessionID string) []string
	ClearQueue(sessionID string)
	Summarize(context.Context, string, fantasy.ProviderOptions) error
	Model() Model
}

type Model struct {
	Model      fantasy.LanguageModel
	CatwalkCfg catwalk.Model
	ModelCfg   config.SelectedModel
}

type sessionAgent struct {
	largeModel         *csync.Value[Model]
	smallModel         *csync.Value[Model]
	systemPromptPrefix *csync.Value[string]
	systemPrompt       *csync.Value[string]
	tools              *csync.Slice[fantasy.AgentTool]

	isSubAgent           bool
	sessions             session.Service
	messages             message.Service
	disableAutoSummarize bool
	notify               pubsub.Publisher[notify.Notification]
	recorder             transcript.Recorder

	messageQueue   *csync.Map[string, []SessionAgentCall]
	activeRequests *csync.Map[string, context.CancelFunc]
}

type SessionAgentOptions struct {
	LargeModel           Model
	SmallModel           Model
	SystemPromptPrefix   string
	SystemPrompt         string
	IsSubAgent           bool
	DisableAutoSummarize bool
	Sessions             session.Service
	Messages             message.Service
	Tools                []fantasy.AgentTool
	Notify               pubsub.Publisher[notify.Notification]
	// Recorder is the transcript seam (Phase 1) wired to the .md writer
	// (Phase 2). When nil, the agent uses transcript.NoopRecorder so
	// Phase 1 runs standalone.
	Recorder transcript.Recorder
}

func NewSessionAgent(
	opts SessionAgentOptions,
) SessionAgent {
	rec := opts.Recorder
	if rec == nil {
		rec = transcript.NoopRecorder{}
	}
	return &sessionAgent{
		largeModel:           csync.NewValue(opts.LargeModel),
		smallModel:           csync.NewValue(opts.SmallModel),
		systemPromptPrefix:   csync.NewValue(opts.SystemPromptPrefix),
		systemPrompt:         csync.NewValue(opts.SystemPrompt),
		isSubAgent:           opts.IsSubAgent,
		sessions:             opts.Sessions,
		messages:             opts.Messages,
		disableAutoSummarize: opts.DisableAutoSummarize,
		tools:                csync.NewSliceFrom(opts.Tools),
		notify:               opts.Notify,
		recorder:             rec,
		messageQueue:         csync.NewMap[string, []SessionAgentCall](),
		activeRequests:       csync.NewMap[string, context.CancelFunc](),
	}
}
