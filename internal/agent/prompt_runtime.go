package agent

import "fmt"

// Re-prompts feed back as the next user-role observation, prefixed [runtime].
// Format per flicknote 30666153 §6I:
//
//	[runtime] <one-line description>
//	<context, if any>
//	<corrective guidance>

// rePromptEmpty is the next-observation text after an empty/whitespace emit.
func rePromptEmpty() string {
	return `[runtime] your last response was empty. emit a bash command, a comment (# ...), a narrate heredoc, or "exit" to end the turn.`
}

// rePromptInvalidBash is the next-observation text after `bash -n` rejected
// the emit. bashErr carries the raw stderr from `bash -n`.
//
// In practice the #1 cause of bash-syntax failure is the model emitting
// natural-language prose at the top level (e.g. "Hi! How can I help?")
// because every response is fed to bash -c. The re-prompt leads with that
// hypothesis so the model corrects course on the next turn, then falls back
// to generic-bash-fix guidance.
func rePromptInvalidBash(bashErr string) string {
	return fmt.Sprintf(`[runtime] your last response was not valid bash. bash -n said:
  %s

THE MOST LIKELY CAUSE: you emitted natural-language prose (a greeting, an
explanation, a markdown answer) instead of bash. Every response is run as
bash -c — there is no chat channel. To say something to the human, wrap
prose in a narrate heredoc:

  narrate <<'EOF'
  your message here — apostrophes, "quotes", $vars all pass through verbatim.
  EOF

To end the turn, emit literally:  exit
To combine: end the heredoc, then  exit  on its own line, OR append  && exit
to the heredoc opener (the heredoc body is the only thing the runtime
parses as natural language; everywhere else, plain text fails).

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
	return fmt.Sprintf(`[runtime] your last command exited with 127 (command not found).
the first word `+"`%s`"+` was not recognized as a command.

if `+"`%s`"+` is a real binary you expected:
  command -v %s     # builtin probe — returns 1 (not 127) if missing
then either install it, or pick an alternative.

if `+"`%s`"+` looks like part of an English sentence ("Let me ...", "I'll ...",
"Here's ...") OR you wrapped your command in a markdown fence
(`+"```bash ... ```"+`), DROP THAT shape:
  - the runtime parses your ENTIRE response as bash via bash -c
  - English prose at the top runs as commands and fails
  - markdown fences (`+"```...```"+`) are bash command-substitution syntax,
    not chat-rendering boundaries

to talk to the human:  narrate <<'EOF' ... EOF
to end the turn:       exit
to act:                emit pure bash (chained with && / ; / | as needed).`,
		firstWord, firstWord, firstWord, firstWord)
}
