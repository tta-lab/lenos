package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tta-lab/lenos/internal/agent/prompt"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/taskwarrior"
)

func isNativeCoderAgent(store *config.ConfigStore) bool {
	switch store.Overrides().AgentName {
	case "", config.AgentCoder:
		return true
	default:
		return false
	}
}

func buildRuntimeContextCommandsForAgent(store *config.ConfigStore, runtimeContext prompt.RuntimeContext) []RuntimeContextCommand {
	if isNativeCoderAgent(store) {
		return buildRuntimeContextCommands(
			runtimeContext,
			config.AgentCoder,
			generalRuntimeContextPromptTmpl,
			contextFilesRuntimeContextPromptTmpl,
			coderRuntimeContextPromptTmpl,
			reviewerRuntimeContextPromptTmpl,
		)
	}
	switch store.Overrides().AgentName {
	case config.AgentReviewer:
		return buildRuntimeContextCommands(
			runtimeContext,
			config.AgentReviewer,
			generalRuntimeContextPromptTmpl,
			contextFilesRuntimeContextPromptTmpl,
			coderRuntimeContextPromptTmpl,
			reviewerRuntimeContextPromptTmpl,
		)
	default:
		return nil
	}
}

// resolveSandbox returns the sandbox setting, defaulting to true if nil.
func resolveSandbox(p *bool) bool {
	if p == nil {
		return true
	}
	return *p
}

func getCoderContextPaths(store *config.ConfigStore) []string {
	if coder, ok := store.Config().Agents[config.AgentCoder]; ok {
		return coder.ContextPaths
	}
	return nil
}

func agentNameOr(name string) string {
	if name != "" {
		return name
	}
	return "lenos"
}

// buildCall assembles the per-turn SessionAgentCall with sandbox env, allowed
// paths, and provider options. Extracted so the OAuth/API-key refresh path
// can rebuild a call with fresh credentials without duplicating wiring.
func buildCall(ctx context.Context, sessionID, userPrompt string, model Model, providerCfg config.ProviderConfig, cfg *config.ConfigStore, sessions session.Service, messages message.Service) (SessionAgentCall, error) {
	sandboxEnv := make(map[string]string, len(os.Environ()))
	for _, e := range os.Environ() {
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			sandboxEnv[e[:idx]] = e[idx+1:]
		}
	}

	cwd := cfg.WorkingDir()
	runtimeContext := prompt.LoadRuntimeContext(ctx, cfg, getCoderContextPaths(cfg))

	useSandbox := resolveSandbox(cfg.Config().Options.Sandbox)
	if cfg.Overrides().NoSandbox {
		useSandbox = false
	}

	access := AccessModeRW
	if cfg.Overrides().ReadOnly {
		access = AccessModeRO
	}

	// Create journal for native coder sessions only.
	var journalPath string
	if isNativeCoderAgent(cfg) {
		if path, err := CreateJournal(cwd, sessionID); err != nil {
			slog.Warn("Failed to create session journal", "error", err)
		} else {
			journalPath = path
			// Expose journal to the subprocess runner via env var.
			sandboxEnv["LENOS_JOURNAL"] = journalPath
			sandboxEnv["LENOS_SESSION_ID"] = sessionID
		}
	}

	// Create goal file when goal text or goal file is provided.
	var goalPath string
	if cfg.Overrides().GoalText != "" || cfg.Overrides().GoalFile != "" {
		var body string
		createdAt := time.Now().Format(time.RFC3339)
		if cfg.Overrides().GoalText != "" {
			body = cfg.Overrides().GoalText
		} else {
			data, err := os.ReadFile(cfg.Overrides().GoalFile)
			if err != nil {
				return SessionAgentCall{}, fmt.Errorf("failed to read goal file %s: %w", cfg.Overrides().GoalFile, err)
			}
			body = string(data)
			// Strip existing frontmatter from the source file; the managed
			// file gets its own normalized frontmatter.
			body = stripYAMLFrontmatter(body)
		}
		if body != "" {
			path, err := CreateGoal(cwd, sessionID, body, createdAt)
			if err != nil {
				return SessionAgentCall{}, fmt.Errorf("failed to create goal file: %w", err)
			}
			goalPath = path
			sandboxEnv["LENOS_GOAL"] = goalPath
		}
	}
	// Auto-detect an existing goal file on disk (created e.g. via TUI "Open Goal").
	if goalPath == "" {
		diskPath := GoalPath(cwd, sessionID)
		if _, err := os.Stat(diskPath); err == nil {
			goalPath = diskPath
			sandboxEnv["LENOS_GOAL"] = goalPath
		}
	}

	dataDir := filepath.Join(cwd, cfg.Config().Options.DataDirectory)
	var trajectoryMaterializer *TrajectoryMaterializer
	if path := TrajectoryPathFromContext(ctx); path != "" {
		trajectoryMaterializer = NewTrajectoryMaterializer(path, sessions, messages, model.messageModelID())
	}

	return SessionAgentCall{
		SessionID:              sessionID,
		Prompt:                 userPrompt,
		usageSummary:           RunUsageSummaryFromContext(ctx),
		trajectoryMaterializer: trajectoryMaterializer,
		ProviderOptions:        getProviderOptions(model, providerCfg),
		PairWith:               cfg.Overrides().PairWith,
		Sandbox:                useSandbox,
		Env:                    sandboxEnv,
		AllowedPaths:           BuildAllowedPaths(ctx, cwd, access),
		TaskID:                 taskwarrior.ResolveTaskID(cwd),
		ContextCommands:        buildRuntimeContextCommandsForAgent(cfg, runtimeContext),
		JournalPath:            journalPath,
		GoalPath:               goalPath,
		GoalStartupHint:        goalPath != "" && (cfg.Overrides().GoalText != "" || cfg.Overrides().GoalFile != ""),
		BashOutput:             cfg.Config().Options.BashOutput,
		DataDir:                dataDir,
	}, nil
}
