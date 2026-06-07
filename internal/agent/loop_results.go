package agent

import (
	"fmt"
	"html"

	"github.com/tta-lab/lenos/internal/agent/lenosbash"
)

func formatResultForModel(_ string, stdout, stderr string, exitCode int) string {
	body := html.EscapeString(stdout)
	if stderr != "" {
		body += "\nSTDERR:\n" + html.EscapeString(stderr)
	}
	if body == "" {
		body = "Bash completed with no output"
	}
	if exitCode != 0 && exitCode != -1 {
		body += fmt.Sprintf("\n(exit code: %d)", exitCode)
	}
	return lenosbash.ResultBlock(body)
}
