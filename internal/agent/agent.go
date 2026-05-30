// Package agent is the core orchestration layer for Lenos AI agents.
//
// It provides session-based AI agent functionality for managing
// conversations and message handling. It coordinates interactions between
// language models, messages, and sessions while handling features like
// automatic summarization, queuing, and token management.
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
	"github.com/tta-lab/lenos/internal/hooks"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/pubsub"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/version"
)

const (
	DefaultSessionName = "Untitled Session"

	// Constants for auto-summarization thresholds.
	contextWindowBufferRatio       = 0.2
	recentUserMessagesAfterCompact = 3
	autoCompactContinuationPrefix  = "The previous session was interrupted because it got too long"
)

// shouldAutoCompact returns true when the session has approached the
// auto-summarization threshold. The remaining-token reserve scales with the
// context window so the summary request still has room for the runtime prompt.
func shouldAutoCompact(contextWindow, used int64) bool {
	if contextWindow <= 0 {
		return false
	}
	remaining := contextWindow - used
	threshold := int64(float64(contextWindow) * contextWindowBufferRatio)
	return remaining <= threshold
}

var userAgent = fmt.Sprintf("Lenos/%s (https://github.com/tta-lab/lenos)", version.Version)

//go:embed templates/summary.md
var summaryPrompt []byte

// SessionAgentCall carries one user-initiated turn through the agent loop.
// It bundles the session ID, prompt, and per-turn runtime context (provider
// options, sandbox env, allowed paths).
type SessionAgentCall struct {
	SessionID string
	Prompt    string

	// ProviderOptions are the per-provider streaming options merged from
	// catwalk + provider config + model config (anthropic thinking, openai
	// reasoning_effort, etc).
	ProviderOptions fantasy.ProviderOptions

	// PairWith is retained for callers that need a default external recipient.
	PairWith string

	// Sandbox controls runner selection. When true and SandboxClient is set
	// the loop runs each emit through temenos; otherwise it falls back to
	// LocalRunner with a clear warning.
	Sandbox       bool
	SandboxClient *client.Client

	// Env is the explicit environment overlay for each subprocess. The
	// coordinator sets session context for the agent loop
	// the session .md path; the data directory is auto-discovered via
	// fsext.LookupClosest from cwd, so the loop does not need to add it.
	Env map[string]string

	// AllowedPaths is the read/write bound for the runner. The first entry
	// also becomes the subprocess working directory.
	AllowedPaths []client.AllowedPath

	// TaskID is the resolved ttal task ID for task-backed sessions. Empty
	// means the session is not task-backed and title refresh is skipped.
	TaskID string

	// ContextCommands are runner-backed context reads persisted before the
	// first user turn so they replay like normal assistant command/result pairs.
	ContextCommands []RuntimeContextCommand
}

type RuntimeContextCommand struct {
	Command  string
	Optional bool
}

type SessionAgent interface {
	Run(context.Context, SessionAgentCall) error
	SetModels(large Model, small Model, primary Model)
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

func (m Model) messageModelID() string {
	if m.ModelCfg.Model != "" {
		return m.ModelCfg.Model
	}
	if m.Model != nil {
		return m.Model.Model()
	}
	return ""
}

func (m Model) messageProviderID() string {
	if m.ModelCfg.Provider != "" {
		return m.ModelCfg.Provider
	}
	if m.Model != nil {
		return m.Model.Provider()
	}
	return ""
}

type sessionAgent struct {
	largeModel   *csync.Value[Model]
	smallModel   *csync.Value[Model]
	primaryModel *csync.Value[Model]
	systemPrompt *csync.Value[string]

	isSubAgent           bool
	sessions             session.Service
	messages             message.Service
	disableAutoSummarize bool
	notify               pubsub.Publisher[notify.Notification]

	messageQueue   *csync.Map[string, []SessionAgentCall]
	activeRequests *csync.Map[string, context.CancelFunc]
	hookRunner     hooks.Runner
	taskExporter   taskTitleExporter
}

type SessionAgentOptions struct {
	LargeModel           Model
	SmallModel           Model
	PrimaryModel         Model
	SystemPrompt         string
	IsSubAgent           bool
	DisableAutoSummarize bool
	Sessions             session.Service
	Messages             message.Service
	Notify               pubsub.Publisher[notify.Notification]
	// HookRunner is called after each model step with a JSON envelope on
	// stdin. Nil-safe: when nil, no post-step hook runs.
	HookRunner hooks.Runner
}

func NewSessionAgent(
	opts SessionAgentOptions,
) SessionAgent {
	return &sessionAgent{
		largeModel:           csync.NewValue(opts.LargeModel),
		smallModel:           csync.NewValue(opts.SmallModel),
		primaryModel:         csync.NewValue(opts.PrimaryModel),
		systemPrompt:         csync.NewValue(opts.SystemPrompt),
		isSubAgent:           opts.IsSubAgent,
		sessions:             opts.Sessions,
		messages:             opts.Messages,
		disableAutoSummarize: opts.DisableAutoSummarize,
		notify:               opts.Notify,
		messageQueue:         csync.NewMap[string, []SessionAgentCall](),
		activeRequests:       csync.NewMap[string, context.CancelFunc](),
		hookRunner:           opts.HookRunner,
		taskExporter:         exportTaskForTitle,
	}
}
