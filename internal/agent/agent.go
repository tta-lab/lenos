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
	"sync"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
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
)

var userAgent = fmt.Sprintf("Lenos/%s (https://github.com/tta-lab/lenos)", version.Version)

// SessionAgentCall carries one user-initiated turn through the agent loop.
// It bundles the session ID, prompt, and per-turn runtime context (provider
// options, sandbox env, allowed paths).
type SessionAgentCall struct {
	SessionID string
	Prompt    string
	// runtimePrompt marks Prompt as runtime feedback persisted as a runtime
	// message, not as a user-visible chat row.
	runtimePrompt bool
	turnPrompts   []turnPrompt
	usageSummary  *RunUsageSummary

	// ProviderOptions are the per-provider streaming options merged from
	// catwalk + provider config + model config (anthropic thinking, openai
	// reasoning_effort, etc).
	ProviderOptions fantasy.ProviderOptions

	// PairWith is retained for callers that need a default external recipient.
	PairWith string

	// Sandbox controls runner selection. When true, the loop uses SandboxedRunner
	// which calls the temenos sandbox SDK directly (no daemon or socket).
	Sandbox bool

	// Env is the explicit environment overlay for each subprocess. The
	// coordinator sets session context for the agent loop
	// the session .md path; the data directory is auto-discovered via
	// fsext.LookupClosest from cwd, so the loop does not need to add it.
	Env map[string]string

	// AllowedPaths is the read/write bound for the runner. The first entry
	// also becomes the subprocess working directory.
	AllowedPaths []AllowedPath

	// TaskID is the resolved ttal task ID for task-backed sessions. Empty
	// means the session is not task-backed and title refresh is skipped.
	TaskID string

	// ContextCommands are runner-backed context reads persisted before the
	// first user turn so they replay like normal assistant command/result pairs.
	ContextCommands []RuntimeContextCommand

	// JournalPath is the absolute path to the per-session journal file. Empty
	// for non-task sessions (reviewer, sub-agent, chat-only).
	JournalPath string

	// GoalPath is the absolute path to the per-session goal file. Empty when
	// no goal is set for this session.
	GoalPath string

	// GoalStartupHint indicates the goal startup runtime hint should be
	// injected on this turn. Set when a goal file is newly created (CLI
	// --goal/--goal-file) or opened/edited (TUI "Open Goal"). The hint is
	// idempotent and only fires once at the first turn where the goal
	// becomes active; ordinary user messages do not set this.
	GoalStartupHint bool

	// MarkCompactBoundary marks the assistant response from this call as a
	// compaction boundary. After this turn, only messages after the boundary
	// are loaded into the context window, giving the agent a fresh start.
	MarkCompactBoundary bool

	// BashOutput configures output bounding for bash commands. Nil means
	// no bounding (output is never truncated). When non-nil, MaxLines
	// and MaxBytes are used to determine truncation.
	BashOutput *config.BashOutputConfig

	// DataDir is the absolute path to the Lenos data directory (e.g., .lenos).
	DataDir string
}

type RuntimeContextCommand struct {
	Command  string
	Optional bool
}

type turnPrompt struct {
	Text    string
	Persist bool
	Role    message.MessageRole
}

type SessionAgent interface {
	Run(context.Context, SessionAgentCall) error
	PrefillContext(context.Context, SessionAgentCall) error
	SetModels(large Model, small Model, primary Model)
	SetSystemPrompt(systemPrompt string)
	Cancel(sessionID string)
	CancelAll()
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	QueuedPrompts(sessionID string) int
	QueuedPromptsList(sessionID string) []string
	ClearQueue(sessionID string)
	ActiveBackgroundJobs(sessionID string) []BackgroundJob
	KillBackgroundJob(ctx context.Context, sessionID, jobID string) error
	StopBackgroundJobs(sessionID string)
	Model() Model
	// CompactSession sends a journal handoff prompt and marks the assistant
	// response as a compaction boundary so subsequent turns start fresh.
	CompactSession(ctx context.Context, call SessionAgentCall) error
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

	isSubAgent bool
	sessions   session.Service
	messages   message.Service
	notify     pubsub.Publisher[notify.Notification]

	messageQueue    *csync.Map[string, []SessionAgentCall]
	activeRequests  *csync.Map[string, context.CancelFunc]
	bgRunners       *csync.Map[string, *BackgroundRunner]
	bgRunnersMu     sync.Mutex
	sessionUpdateMu sync.Mutex
	hookRunner      hooks.Runner
	taskExporter    taskTitleExporter
}

type SessionAgentOptions struct {
	LargeModel   Model
	SmallModel   Model
	PrimaryModel Model
	SystemPrompt string
	IsSubAgent   bool
	Sessions     session.Service
	Messages     message.Service
	Notify       pubsub.Publisher[notify.Notification]
	// HookRunner is called after each model step with a JSON envelope on
	// stdin. Nil-safe: when nil, no post-step hook runs.
	HookRunner hooks.Runner
}

func NewSessionAgent(
	opts SessionAgentOptions,
) SessionAgent {
	return &sessionAgent{
		largeModel:     csync.NewValue(opts.LargeModel),
		smallModel:     csync.NewValue(opts.SmallModel),
		primaryModel:   csync.NewValue(opts.PrimaryModel),
		systemPrompt:   csync.NewValue(opts.SystemPrompt),
		isSubAgent:     opts.IsSubAgent,
		sessions:       opts.Sessions,
		messages:       opts.Messages,
		notify:         opts.Notify,
		messageQueue:   csync.NewMap[string, []SessionAgentCall](),
		activeRequests: csync.NewMap[string, context.CancelFunc](),
		bgRunners:      csync.NewMap[string, *BackgroundRunner](),
		hookRunner:     opts.HookRunner,
		taskExporter:   exportTaskForTitle,
	}
}
