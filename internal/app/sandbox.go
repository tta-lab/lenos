package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/temenos/client"
)

const sandboxHealthProbeTimeout = 1 * time.Second

// initSandboxClient returns a temenos client per the three-state policy:
//
//   - opts.Sandbox == nil (default true): build + probe; on fail, warn + (nil, nil).
//   - opts.Sandbox == &true (explicit):   build + probe; on fail, return error.
//   - opts.Sandbox == &false:             skip; (nil, nil).
//
// Empty endpoint resolves through temenos's env chain (TEMENOS_LISTEN_ADDR →
// TEMENOS_SOCKET_PATH → ~/.temenos/daemon.sock).
func initSandboxClient(ctx context.Context, opts *config.Options) (*client.Client, error) {
	if opts == nil {
		slog.Warn("sandbox config unavailable; falling back to LocalRunner")
		return nil, nil
	}
	if opts.Sandbox != nil && !*opts.Sandbox {
		return nil, nil
	}
	explicit := opts.Sandbox != nil && *opts.Sandbox

	c, err := client.New(opts.SandboxEndpoint)
	if err != nil {
		if explicit {
			return nil, fmt.Errorf("sandbox=true but temenos client construction failed: %w", err)
		}
		slog.Warn("temenos client construction failed; falling back to LocalRunner",
			"endpoint", opts.SandboxEndpoint, "err", err)
		return nil, nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, sandboxHealthProbeTimeout)
	defer cancel()
	if err := c.Health(probeCtx); err != nil {
		if explicit {
			return nil, fmt.Errorf("sandbox=true but temenos daemon unreachable at %q: %w",
				effectiveEndpoint(opts.SandboxEndpoint), err)
		}
		slog.Warn("temenos daemon unreachable; falling back to LocalRunner",
			"endpoint", effectiveEndpoint(opts.SandboxEndpoint), "err", err)
		return nil, nil
	}
	return c, nil
}

// effectiveEndpoint returns the user-facing endpoint string for log/error messages.
// When endpoint is empty, the temenos client resolves env at New() time; we keep
// the empty string visible here ("(default)") to make logs self-explanatory.
func effectiveEndpoint(endpoint string) string {
	if endpoint == "" {
		return "(default: TEMENOS_LISTEN_ADDR/TEMENOS_SOCKET_PATH/~/.temenos/daemon.sock)"
	}
	return endpoint
}
