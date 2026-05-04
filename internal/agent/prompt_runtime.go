package agent

import "fmt"

// Re-prompts feed back as the next user-role observation. Two prefix tiers:
//
//   - [runtime] — informational correction (empty, invalid-bash, banned, timeout).
//     bash already ran (or was cleanly blocked before any side-effects).
//   - [ALERT from runtime] — high-salience correction where the model has
//     demonstrated it ignores trailing corrections (cmd-not-found, prose-prefix).
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
	return fmt.Sprintf(alertPrefix+` bash printed "command not found" for the first word `+"`%s`"+`.

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

to annotate one command (one line):  # this is a bash comment — bash ignores it
to talk to the human (multi-line):   narrate <<'EOF' ... EOF
to end the turn:                     exit
to act:                              emit pure bash (chained with && / ; / | as needed).`,
		firstWord, firstWord, firstWord, firstWord)
}

// rePromptProsePrefix is the next-observation text after the runtime detected
// a Title-Cased prose word at the start of an emit (typically "Let", "Now",
// "Read", "The", etc — common English sentence-openers). The runtime never
// executed the emit; bash was bypassed so the model gets a clean, unambiguous
// signal that the shape was wrong before any side-effects could happen.
//
// Quotes the actual offending line and shows the in-place conversion to
// bash comment + narrate forms — model sees the exact text it should have
// written instead of the abstract rule.
func rePromptProsePrefix(firstWord, line string) string {
	return fmt.Sprintf(alertPrefix+` your last emit started with English prose:

  %s

The runtime DID NOT execute it — every byte of your response is fed to bash -c, and English sentences run as commands (which fail with "command not found"). To prevent any side effects, no bash ran this turn.

If this was meant as a brief note before a command, convert to a bash comment:
  # %s

If this was meant as a multi-line message to the human, wrap in narrate:
  narrate <<'EOF'
  %s
  EOF

To act, emit pure bash starting with a lowercase command (ls, grep, src, etc.).
To end the turn, emit literally:  exit

If `+"`%s`"+` was actually a real binary (e.g. cap-named tools like Cargo), probe with:
  command -v %s

then re-emit with the verified path.`,
		line, line, line, firstWord, firstWord)
}
