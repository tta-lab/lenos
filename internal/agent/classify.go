package agent

import (
	"context"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
)

// classifyResult enumerates how the loop should handle a model emit.
type classifyResult int

const (
	classifyExec     classifyResult = iota
	classifyExit                    // emit IS the exit (no command to run)
	classifyExecExit                // emit ends in `<sep> exit` — run, then exit
	classifyEmpty
	classifyInvalidBash
	classifyBanned
	classifyProsePrefix // emit starts with Title-Cased prose word
)

// exitRe matches a literal `exit` / `exit N` (with optional integer and
// surrounding whitespace including tabs). Multi-line strings, exit followed
// by other commands, or "exit" inside a quoted string never match because
// classify() trims the input first and only this regex is applied.
var exitRe = regexp.MustCompile(`^\s*exit(\s+-?\d+)?\s*$`)

// trailingExitRe matches an emit whose final command is `exit` joined by a
// shell separator: `... && exit`, `... ; exit`, `... || exit`, or a newline
// (e.g. heredoc body followed by `exit` on the next line). The model uses
// this idiom to combine an action (typically `narrate "..."`) with turn-end
// in a single response — common enough that ignoring the exit signal would
// force every turn into a redundant follow-up emit. We strip the trailing
// exit clause and run the command portion via classifyExec, then the loop
// returns stopExit instead of continuing.
var trailingExitRe = regexp.MustCompile(`(?:&&|\|\||;|\n)\s*exit(?:\s+-?\d+)?\s*$`)

// blockedCmdPatterns guards in-place file edits (sed -i / perl -i). CC native
// sandbox is the dominant defense; this is a thin nudge to push agents toward
// `src edit` for in-place file modifications.
var blockedCmdPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)(?:^|&&|\|\||;|\|)\s*sed\s+(?:-[a-zA-Z]*i|--in-place)`),
	regexp.MustCompile(`(?m)(?:^|&&|\|\||;|\|)\s*perl\s+(?:-[a-zA-Z]*i)`),
}

// classify inspects an agent emit and returns the action class plus an
// auxiliary string (bash stderr for classifyInvalidBash; first Title-Cased
// word for classifyProsePrefix; "" otherwise).
//
// Classification order: empty → exit → banned → bash-syntax → prose-prefix → trailing-exit → exec.
// Empty short-circuits before exit so `   ` doesn't accidentally pass the
// trim+exitRe check; banned runs before bash-syntax so we never invoke
// `bash -n` on a refused pattern; prose-prefix runs after bash-syntax so
// true syntax errors still win.
func classify(ctx context.Context, emit string) (cls classifyResult, bashErr string) {
	trimmed := strings.TrimSpace(emit)
	if trimmed == "" {
		return classifyEmpty, ""
	}
	if exitRe.MatchString(trimmed) {
		return classifyExit, ""
	}
	if containsBlockedPattern(emit) {
		return classifyBanned, ""
	}
	if err := bashSyntaxCheck(ctx, emit); err != "" {
		return classifyInvalidBash, err
	}
	// Pre-exec prose detection. Catches "Read the file..." shape that
	// passes bash -n but starts with English prose. Word returned via
	// bashErr slot for routing; the loop branch calls detectProsePrefix
	// again to also obtain the offending line for the re-prompt body.
	if word, _ := detectProsePrefix(emit); word != "" {
		return classifyProsePrefix, word
	}
	if trailingExitRe.MatchString(trimmed) {
		return classifyExecExit, ""
	}
	return classifyExec, ""
}

func containsBlockedPattern(emit string) bool {
	for _, re := range blockedCmdPatterns {
		if re.MatchString(emit) {
			return true
		}
	}
	return false
}

// proseFirstWordRe matches a Title-Cased English word at the start of a line
// (after optional whitespace). UNIX commands are lowercase by convention; a
// leading [A-Z][a-z]+ token is almost always English prose leaking into the
// bash channel. Lowercase prose openings are deliberately not detected —
// sample evidence shows model prose almost always starts sentence-case, and
// lowercase commands are conventional UNIX, so capital-letter is a clean signal.
var proseFirstWordRe = regexp.MustCompile(`^([A-Z][a-z]+)\b`)

// detectProsePrefix returns the Title-Cased first word and the full first
// non-comment, non-whitespace line of emit, or ("", "") if no match. Comment
// lines (start with `#`) are skipped since bash ignores them and they don't leak.
//
// The full line is returned alongside the first word so re-prompts can quote
// the model's exact prose verbatim and show the in-place conversion to bash
// comment + narrate forms — direct conversion lowers the cognitive friction
// in correcting next turn vs an abstract rule restatement.
//
// Heuristic: false positives possible on cap-named binaries (e.g. macOS
// /usr/bin/Read, Cargo) but the prose re-prompt is constructive in those
// cases — it asks the model to probe with `command -v <X>` and re-emit.
func detectProsePrefix(emit string) (firstWord, line string) {
	for _, candidate := range strings.Split(emit, "\n") {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if m := proseFirstWordRe.FindStringSubmatch(trimmed); m != nil {
			return m[1], trimmed
		}
		return "", "" // first content line wasn't Title-Cased — accept the emit
	}
	return "", ""
}

// bashSyntaxCheck runs `bash -n` against the emit on stdin. Returns "" on
// valid syntax, the captured stderr on invalid. A subprocess-level failure
// (binary missing, signal kill) is logged and treated as valid — the runtime
// shouldn't block the agent on host-level breakage that's outside its control.
func bashSyntaxCheck(ctx context.Context, emit string) string {
	cmd := exec.CommandContext(ctx, "/bin/bash", "-n")
	cmd.Stdin = strings.NewReader(emit)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() != 0 {
			return strings.TrimSpace(stderr.String())
		}
		slog.Warn("bash -n preflight failed at runtime level; treating as valid", "error", err)
		return ""
	}
	return ""
}
