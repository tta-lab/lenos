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
	return `[runtime] your last response was empty. emit a bash command, a comment (# ...), narrate "message", or "exit" to end the turn.`
}

// rePromptInvalidBash is the next-observation text after `bash -n` rejected
// the emit. bashErr carries the raw stderr from `bash -n`.
func rePromptInvalidBash(bashErr string) string {
	return fmt.Sprintf(`[runtime] your last response was not valid bash. bash -n said:
  %s

if you wanted to say something, use:
  - narrate "message"        (voiceover prose for the human; not a sink for command output)
  - # comment text           (a bash comment, no side effect)

if you wanted to run a command, ensure it's valid bash.`, bashErr)
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
