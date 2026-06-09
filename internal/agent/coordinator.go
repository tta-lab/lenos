package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/tta-lab/lenos/internal/agent/notify"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/hooks"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/pubsub"
	"github.com/tta-lab/lenos/internal/session"
	"golang.org/x/sync/errgroup"
)

// Coordinator errors.
var (
	errCoderAgentNotConfigured          = errors.New("coder agent not configured")
	errModelProviderNotConfigured       = errors.New("model provider not configured")
	errLargeModelNotSelected            = errors.New("large model not selected")
	errSmallModelNotSelected            = errors.New("small model not selected")
	errLargeModelProviderNotConfigured  = errors.New("large model provider not configured")
	errSmallModelProviderNotConfigured  = errors.New("small model provider not configured")
	errReviewModelProviderNotConfigured = errors.New("review model provider not configured")
	errLargeModelNotFound               = errors.New("large model not found in provider config")
	errSmallModelNotFound               = errors.New("small model not found in provider config")
	errReviewModelNotFound              = errors.New("review model not found in provider config")
)

type Coordinator interface {
	// INFO: (kujtim) this is not used yet we will use this when we have multiple agents
	// SetMainAgent(string)
	Run(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) error
	RunRuntime(ctx context.Context, sessionID, prompt string) error
	PrefillContext(ctx context.Context, sessionID string) error
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
	UpdateModels(ctx context.Context) error
	// SystemPrompt returns the fully-resolved system prompt currently sent
	// to the model on every turn. Useful for `lenos system-prompt` and
	// debugging "the model isn't following the protocol" issues.
	SystemPrompt() string
	// CompactSession sends a journal handoff hint to the agent and marks the
	// response as a compaction boundary so the next turn starts fresh.
	CompactSession(ctx context.Context, sessionID string) error
}

type coordinator struct {
	cfg          *config.ConfigStore
	sessions     session.Service
	messages     message.Service
	notify       pubsub.Publisher[notify.Notification]
	currentAgent SessionAgent
	systemPrompt string
	readyWg      errgroup.Group

	dataDir  string
	titleMgr *titleManager
}

func NewCoordinator(
	ctx context.Context,
	cfg *config.ConfigStore,
	sessions session.Service,
	messages message.Service,
	notify pubsub.Publisher[notify.Notification],
) (Coordinator, error) {
	absDataDir, err := filepath.Abs(cfg.Config().Options.DataDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve data dir: %w", err)
	}

	c := &coordinator{
		cfg:      cfg,
		sessions: sessions,
		messages: messages,
		notify:   notify,
		dataDir:  absDataDir,
	}

	if shouldEnableTitle(c.cfg) {
		agentName := agentNameOr(c.cfg.Overrides().AgentName)
		c.titleMgr = newTitleManager(agentName)
		tmuxRenameWindow(agentName)
	}

	large, small, err := buildAgentModels(ctx, false, c.cfg)
	if err != nil {
		return nil, err
	}

	var hookRunner hooks.Runner
	if h := cfg.Config().Hooks; h != nil && h.PostStep != "" {
		hookRunner = hooks.ShellRunner{Command: h.PostStep}
	}

	primary := large
	switch c.cfg.Overrides().ActiveTier {
	case config.SelectedModelTypeSmall:
		primary = small
	case config.SelectedModelTypeReview:
		reviewCfg, ok := c.cfg.Config().Models[config.SelectedModelTypeReview]
		if ok && reviewCfg.Model != "" {
			primary, err = buildSelectedModel(ctx, reviewCfg, false, errReviewModelProviderNotConfigured, errReviewModelNotFound, c.cfg)
			if err != nil {
				return nil, err
			}
		}
	}

	c.currentAgent = NewSessionAgent(SessionAgentOptions{
		LargeModel:   large,
		SmallModel:   small,
		PrimaryModel: primary,
		SystemPrompt: "",
		IsSubAgent:   false,
		Sessions:     sessions,
		Messages:     messages,
		Notify:       notify,
		HookRunner:   hookRunner,
	})

	// Build system prompt: bash-first base + git guidance + lenos.md.tpl
	// (universal rules + identity body + memory tails).
	contextPaths := getCoderContextPaths(c.cfg)
	c.systemPrompt, err = SystemPrompt(
		ctx,
		c.cfg.WorkingDir(),
		large.Model.Provider(),
		large.Model.Model(),
		c.cfg,
		contextPaths,
	)
	if err != nil {
		return nil, fmt.Errorf("build system prompt: %w", err)
	}
	// Push the resolved prompt onto the agent — without this, agent_run.go's
	// a.systemPrompt.Get() returns the empty string passed at NewSessionAgent
	// construction and the model runs with no instructions (which is why the
	// bash-first protocol was being ignored).
	c.currentAgent.SetSystemPrompt(c.systemPrompt)

	return c, nil
}

// Run implements Coordinator.
func (c *coordinator) Run(ctx context.Context, sessionID string, prompt string, attachments ...message.Attachment) error {
	if err := c.readyWg.Wait(); err != nil {
		return err
	}

	prompt = message.PromptWithTextAttachments(prompt, attachments)

	model := c.currentAgent.Model()

	providerCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return errModelProviderNotConfigured
	}

	// Refresh OAuth token if expired before running.
	// Ported from upstream 8cd4786c (extract token refresh helpers).
	if err := maybeRefreshToken(ctx, &model, &providerCfg, c.cfg, c.UpdateModels, c.currentAgent.Model); err != nil {
		return err
	}

	call, err := buildCall(ctx, sessionID, prompt, model, providerCfg, c.cfg)
	if err != nil {
		return fmt.Errorf("build call: %w", err)
	}

	if c.titleMgr != nil {
		c.titleMgr.StartWorking()
		defer c.titleMgr.StopWorking()
	}
	runErr := c.currentAgent.Run(ctx, call)
	if runErr == nil {
		return nil
	}

	if isUnauthorized(runErr) {
		slog.Debug("Received 401, attempting token refresh", "provider", providerCfg.ID)
		switch {
		case providerCfg.OAuthToken != nil:
			if err := refreshOAuth2Token(ctx, providerCfg, c.cfg); err != nil {
				return fmt.Errorf("token refresh failed after 401: %w", err)
			}
		case strings.Contains(providerCfg.APIKeyTemplate, "$"):
			if err := refreshApiKeyTemplate(ctx, providerCfg, c.cfg); err != nil {
				return fmt.Errorf("API key refresh failed after 401: %w", err)
			}
		default:
			return fmt.Errorf("agent.Run: %w", runErr)
		}
		if err := c.UpdateModels(ctx); err != nil {
			return fmt.Errorf("rebuild model after refresh: %w", err)
		}
		freshCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
		if !ok {
			return fmt.Errorf("provider %s not found after refresh", model.ModelCfg.Provider)
		}
		var buildErr error
		call, buildErr = buildCall(ctx, sessionID, prompt, c.currentAgent.Model(), freshCfg, c.cfg)
		if buildErr != nil {
			return fmt.Errorf("build call: %w", buildErr)
		}
		if c.titleMgr != nil {
			c.titleMgr.StartWorking()
			defer c.titleMgr.StopWorking()
		}
		if runErr := c.currentAgent.Run(ctx, call); runErr != nil {
			return fmt.Errorf("agent.Run after refresh: %w", runErr)
		}
		return nil
	}
	return fmt.Errorf("agent.Run: %w", runErr)
}

func (c *coordinator) RunRuntime(ctx context.Context, sessionID, prompt string) error {
	if err := c.readyWg.Wait(); err != nil {
		return err
	}

	model := c.currentAgent.Model()
	providerCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return errModelProviderNotConfigured
	}

	call, err := buildCall(ctx, sessionID, prompt, model, providerCfg, c.cfg)
	if err != nil {
		return fmt.Errorf("build call: %w", err)
	}
	call.runtimePrompt = true
	call.GoalStartupHint = true

	if c.titleMgr != nil {
		c.titleMgr.StartWorking()
		defer c.titleMgr.StopWorking()
	}
	return c.currentAgent.Run(ctx, call)
}

func (c *coordinator) PrefillContext(ctx context.Context, sessionID string) error {
	if err := c.readyWg.Wait(); err != nil {
		return err
	}

	model := c.currentAgent.Model()
	providerCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return errModelProviderNotConfigured
	}

	call, err := buildCall(ctx, sessionID, "context prefill", model, providerCfg, c.cfg)
	if err != nil {
		return fmt.Errorf("build call: %w", err)
	}
	return c.currentAgent.PrefillContext(ctx, call)
}

func (c *coordinator) Cancel(sessionID string) {
	c.currentAgent.Cancel(sessionID)
}

func (c *coordinator) CancelAll() {
	c.currentAgent.CancelAll()
}

func (c *coordinator) ClearQueue(sessionID string) {
	c.currentAgent.ClearQueue(sessionID)
}

func (c *coordinator) IsBusy() bool {
	return c.currentAgent.IsBusy()
}

func (c *coordinator) IsSessionBusy(sessionID string) bool {
	return c.currentAgent.IsSessionBusy(sessionID)
}

func (c *coordinator) Model() Model {
	return c.currentAgent.Model()
}

func (c *coordinator) SystemPrompt() string {
	return c.systemPrompt
}

func (c *coordinator) UpdateModels(ctx context.Context) error {
	// Build the models again so we make sure we get the latest config.
	large, small, err := buildAgentModels(ctx, false, c.cfg)
	if err != nil {
		return err
	}
	primary := large
	switch c.cfg.Overrides().ActiveTier {
	case config.SelectedModelTypeSmall:
		primary = small
	case config.SelectedModelTypeReview:
		reviewCfg, ok := c.cfg.Config().Models[config.SelectedModelTypeReview]
		if ok && reviewCfg.Model != "" {
			primary, err = buildSelectedModel(ctx, reviewCfg, false, errReviewModelProviderNotConfigured, errReviewModelNotFound, c.cfg)
			if err != nil {
				return err
			}
		}
	}
	c.currentAgent.SetModels(large, small, primary)

	// Rebuild the system prompt — the lenos wrapper can vary by
	// provider/model — and push it onto the agent.
	contextPaths := getCoderContextPaths(c.cfg)
	sp, err := SystemPrompt(
		ctx,
		c.cfg.WorkingDir(),
		large.Model.Provider(),
		large.Model.Model(),
		c.cfg,
		contextPaths,
	)
	if err != nil {
		return fmt.Errorf("rebuild system prompt: %w", err)
	}
	c.systemPrompt = sp
	c.currentAgent.SetSystemPrompt(sp)
	return nil
}

func (c *coordinator) QueuedPrompts(sessionID string) int {
	return c.currentAgent.QueuedPrompts(sessionID)
}

func (c *coordinator) QueuedPromptsList(sessionID string) []string {
	return c.currentAgent.QueuedPromptsList(sessionID)
}

func (c *coordinator) ActiveBackgroundJobs(sessionID string) []BackgroundJob {
	return c.currentAgent.ActiveBackgroundJobs(sessionID)
}

func (c *coordinator) KillBackgroundJob(ctx context.Context, sessionID, jobID string) error {
	return c.currentAgent.KillBackgroundJob(ctx, sessionID, jobID)
}

func (c *coordinator) StopBackgroundJobs(sessionID string) {
	c.currentAgent.StopBackgroundJobs(sessionID)
}

// CompactSession sends a journal handoff hint to the agent and marks the
// response as a compaction boundary, giving the next turn a fresh context window.
func (c *coordinator) CompactSession(ctx context.Context, sessionID string) error {
	if err := c.readyWg.Wait(); err != nil {
		return err
	}
	model := c.currentAgent.Model()
	providerCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return errModelProviderNotConfigured
	}
	call, err := buildCall(ctx, sessionID, compactHandoffHint(), model, providerCfg, c.cfg)
	if err != nil {
		return err
	}
	call.runtimePrompt = true
	call.MarkCompactBoundary = true
	return c.currentAgent.CompactSession(ctx, call)
}
