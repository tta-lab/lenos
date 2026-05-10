package agent

import (
	"context"
	"strings"

	"github.com/tta-lab/temenos/client"
)

type runnerBashSalvageProbe struct {
	runner Runner
	env    map[string]string
	paths  []client.AllowedPath
}

func (p runnerBashSalvageProbe) commandExists(ctx context.Context, name string) bool {
	if p.runner == nil || strings.TrimSpace(name) == "" {
		return false
	}
	res := p.runner.Run(ctx, "command -v -- "+shellQuote(name)+" >/dev/null", p.env, p.paths)
	return res.Err == nil && res.ExitCode == 0
}

func (p runnerBashSalvageProbe) pathExecutable(ctx context.Context, path string) bool {
	if p.runner == nil || strings.TrimSpace(path) == "" {
		return false
	}
	script := "p=" + shellQuote(path) + "\n" +
		"case \"$p\" in \"~/\"*) p=\"$HOME/${p#~/}\";; esac\n" +
		"test -x \"$p\""
	res := p.runner.Run(ctx, script, p.env, p.paths)
	return res.Err == nil && res.ExitCode == 0
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
