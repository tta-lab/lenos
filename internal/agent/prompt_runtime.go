package agent

import (
	"fmt"

	"github.com/tta-lab/lenos/internal/agent/lenosbash"
)

// Re-prompts feed back as the next user-role observation in runtime tags.
var alertPrefix = lenosbash.RuntimeTag + " ALERT:"

// rePromptEmpty is the next-observation text after an empty/whitespace emit.
func rePromptEmpty() string {
	return lenosbash.RuntimeLine("your last response was empty. emit Markdown prose, a run block, or `exit` to end the turn.")
}

// rePromptInvalidBash is the next-observation text after the shell parser
// rejected the run block.
func rePromptInvalidBash(parseErr string) string {
	return lenosbash.RuntimeBlock(fmt.Sprintf(`your last run block was not valid bash. the shell parser said:
  %s

Fix the bash quoting. "unexpected
EOF while looking for matching" errors come from unbalanced quotes —
apostrophes inside single quotes close the quote prematurely. Use double
quotes or a heredoc for any text containing apostrophes.`, parseErr))
}

func rePromptInvalidLenosBash(source string, diag lenosbash.Diagnostic) string {
	return lenosbash.RenderDiagnostic(source, diag)
}

// rePromptBlockedPattern is the next-observation text after a sed -i / perl -i
// pattern was matched.
func rePromptBlockedPattern() string {
	return lenosbash.RuntimeBlock(`Blocked: sed -i / perl -i is not allowed in this environment.
Use src edit for file modifications — e.g.:
  src edit <file>
See src --help for usage.`)
}

// rePromptTimeout is the next-observation text after a per-call timeout.
func rePromptTimeout(secs int) string {
	return lenosbash.RuntimeBlock(fmt.Sprintf(`your last command exceeded the per-call timeout (%ds) and was killed.
partial output captured. if the command needed more time, use bash native timeout:
  timeout 30m <command>
or break it into smaller steps.`, secs))
}

// rePromptCmdNotFound is the next-observation text after `bash -c <emit>`
// exited with 127 (command not found). Fires both for legit-missing-tool
// scenarios (model expected a binary that is not installed) AND for
// malformed run blocks where the first word is not a real command.
//
// The re-prompt text covers both interpretations so the model can
// self-diagnose: probe with `command -v <X>` if the binary was expected,
// or drop the prose/fence and emit pure bash if shape was wrong.
func rePromptCmdNotFound(firstWord string) string {
	return lenosbash.AlertLine(fmt.Sprintf(`bash printed "command not found" for the first word `+"`%s`"+`.

if `+"`%s`"+` is a real binary you expected:
  command -v %s     # builtin probe — returns 1 (not 127) if missing
then either install it, or pick an alternative.

If you were trying to talk to the human, put that Markdown prose outside run blocks.
If you were trying to run a command, fix the command name or use an installed
alternative.`, firstWord, firstWord, firstWord))
}
