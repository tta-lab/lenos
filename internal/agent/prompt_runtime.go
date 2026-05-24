package agent

import "fmt"

// Re-prompts feed back as the next user-role observation. Two prefix tiers:
//
//   - [runtime] — informational correction (empty, invalid-bash, banned, timeout).
//     bash already ran (or was cleanly blocked before any side-effects).
//   - [ALERT from runtime] — high-salience correction where the model has
//     demonstrated it ignores trailing corrections (cmd-not-found, tool-call).
//     Uses a distinct prefix and appears BEFORE any result envelope so the model
//     reads the correction first.
//
// alertPrefix is the shared literal for high-salience re-prompts.
const alertPrefix = "[ALERT from runtime]"

// rePromptEmpty is the next-observation text after an empty/whitespace emit.
func rePromptEmpty() string {
	return `[runtime] your last response was empty. emit a bash command, a comment (# ...), a narrate heredoc, or "exit" to end the turn.`
}

// rePromptInvalidBash is the next-observation text after `bash -n` rejected
// the emit. bashErr carries the raw stderr from `bash -n`.
//
// In practice a common cause of bash-syntax failure is the model emitting
// lowercase natural-language prose or quoted text that falls outside the
// natural-language rewrite guard. The re-prompt leads with that hypothesis so
// the model corrects course on the next turn, then falls back to generic bash
// fix guidance.
func rePromptInvalidBash(bashErr string) string {
	return fmt.Sprintf(`[runtime] your last response was not valid bash. bash -n said:
  %s

THE MOST LIKELY CAUSE: you emitted text that is neither bash nor a valid
message heredoc. To say something explicitly, emit:

cat <<'EOF' | narrate
your message here — apostrophes, "quotes", $vars all pass through.
EOF

To end the turn without sending a message, emit literally:  exit

If you actually meant to run a command, fix the bash quoting. "unexpected
EOF while looking for matching" errors come from unbalanced quotes —
apostrophes inside single quotes close the quote prematurely. Use double
quotes or a heredoc for any text containing apostrophes.`, bashErr)
}

// rePromptBlockedPattern is the next-observation text after a sed -i / perl -i
// pattern was matched.
func rePromptBlockedPattern() string {
	return `[runtime] Blocked: sed -i / perl -i is not allowed in this environment.
Use src edit for file modifications — e.g.:
  src edit <file>
See src --help for usage.`
}

// rePromptToolCall is the next-observation text after the model emitted a
// structured tool/function call shape. This runtime has no tool-calling API:
// every turn is either plain bash, narrate, comments, or exit.
//
// Body deliberately avoids spelling out the wrong shapes verbatim. Quoting
// literal wrappers such as XML / bracket tool-call forms would re-inject the
// same pattern we just deleted from assistant history in the tool-call branch.
// The description stays abstract; the correct bash / narrate / comment / exit
// shapes are still demonstrated concretely because those are the patterns we
// want the model to copy.
func rePromptToolCall() string {
	return alertPrefix + ` your last emit used a tool/function call format.

There is NO tool/function calling API in this environment. Any structured
wrapper around bash commands — XML tags, JSON envelopes, or bracket
notation — is discarded and never executed.

To act, emit plain bash only:
  ls -la
  rg "needle" .
  src edit internal/agent/loop.go

To talk to the human, use:
cat <<'EOF' | narrate
your message here
EOF

To leave a short note before a command, use a bash comment:
  # checking the agent loop

To end the turn, emit literally:
  exit`
}

// rePromptTimeout is the next-observation text after a per-call timeout.
func rePromptTimeout(secs int) string {
	return fmt.Sprintf(`[runtime] your last command exceeded the per-call timeout (%ds) and was killed.
partial output captured. if the command needed more time, use bash native timeout:
  timeout 30m <command>
or break it into smaller steps.`, secs)
}

// rePromptCmdNotFound is the next-observation text after `bash -c <emit>`
// exited with 127 (command not found). Fires both for legit-missing-tool
// scenarios (model expected a binary that is not installed) AND for
// chat-style shape failures (model emitted prose or fenced markdown
// where the first word — or the cmd-sub captured output's first word —
// is not a real command).
//
// The re-prompt text covers both interpretations so the model can
// self-diagnose: probe with `command -v <X>` if the binary was expected,
// or drop the prose/fence and emit pure bash if shape was wrong.
func rePromptCmdNotFound(firstWord string) string {
	return fmt.Sprintf(alertPrefix+` bash printed "command not found" for the first word `+"`%s`"+`.

if `+"`%s`"+` is a real binary you expected:
  command -v %s     # builtin probe — returns 1 (not 127) if missing
then either install it, or pick an alternative.

if `+"`%s`"+` looks like part of an English sentence ("let me ...", "i'll ...",
"here's ...") OR you wrapped your command in a markdown fence, drop that shape:
  - the runtime parses your ENTIRE response as bash via bash -c
  - shell-looking prose runs as commands and fails
  - markdown fences are command-substitution syntax, not chat-rendering
    boundaries

to annotate one command (one line):  # this is a bash comment — bash ignores it
to talk to the human (multi-line):   cat <<'EOF' | narrate ... EOF
to end the turn without text:        exit
to act:                              emit pure bash (chained with && / ; / | as needed).`,
		firstWord, firstWord, firstWord, firstWord)
}
