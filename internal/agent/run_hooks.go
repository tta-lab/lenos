package agent

import (
	"context"
	"log/slog"
	"time"

	"charm.land/fantasy"

	"github.com/tta-lab/lenos/internal/hooks"
)

// hookTimeout is the per-invocation deadline for post_step hooks. Var (not
// const) so tests can shrink it via export_test.go.
var hookTimeout = 5 * time.Second

// buildPostStepHook builds and fires the configured post_step hook, if any.
// Runs in a goroutine with hookTimeout deadline; errors are logged at WARN but
// never abort the loop.
func (a *sessionAgent) buildPostStepHook(call SessionAgentCall, model Model) func(int, fantasy.Usage, fantasy.ProviderMetadata) {
	if a.hookRunner == nil {
		return nil
	}
	runner := a.hookRunner
	sessionID := call.SessionID
	modelID := model.Model.Model()
	contextWindow := int(model.CatwalkCfg.ContextWindow)
	return func(stepIdx int, u fantasy.Usage, m fantasy.ProviderMetadata) {
		payload, err := hooks.MarshalPostStep(stepIdx, sessionID, modelID, contextWindow, u, time.Now(), usageCost(model, u, a.openrouterCost(m)))
		if err != nil {
			slog.Warn("post_step: marshal envelope", "session", sessionID, "step", stepIdx, "error", err)
			return
		}
		timeout := hookTimeout // capture at closure-execution time, before spawning goroutine
		go func() {
			hookCtx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			if err := runner.Run(hookCtx, payload); err != nil {
				slog.Warn("post_step: runner failed", "session", sessionID, "step", stepIdx, "error", err)
			}
		}()
	}
}
