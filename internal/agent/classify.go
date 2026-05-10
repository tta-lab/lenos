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
	classifyExec classifyResult = iota
	classifyExit                // emit IS the exit (no command to run)
	classifyEmpty
	classifyToolCall
	classifyInvalidBash
	classifyBanned
	classifyMdExit     // :md routes, then exits by default
	classifyMdContinue // :md routes, then continues when body ends with :continue
	classifyNaturalLanguage
)

// exitRe matches a literal `exit` / `exit N` (with optional integer and
// surrounding whitespace including tabs). Multi-line strings, exit followed
// by other commands, or "exit" inside a quoted string never match because
// classify() trims the input first and only this regex is applied.
var exitRe = regexp.MustCompile(`^\s*exit(\s+-?\d+)?\s*$`)

// mdTrailingContinueRe matches `\n:continue` at the very end of an emit.
// :md exits by default; this marker is the only :md continue signal.
var mdTrailingContinueRe = regexp.MustCompile(`\n:continue\s*$`)

// toolCallXMLRe matches XML-style tool/function call hallucinations.
var toolCallXMLRe = regexp.MustCompile(`(?i)</?(?:tool_call|minimax:tool_call|function_call|tool_use|invoke)\b[^>]*>`)

// toolCallBracketRe matches bracket-style tool/function call hallucinations.
var toolCallBracketRe = regexp.MustCompile(`(?i)\[/?(?:tool_?call|function_?call|tool_?use|invoke)\]`)

// blockedCmdPatterns guards in-place file edits (sed -i / perl -i). CC native
// sandbox is the dominant defense; this is a thin nudge to push agents toward
// `src edit` for in-place file modifications.
var blockedCmdPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)(?:^|&&|\|\||;|\|)\s*sed\s+(?:-[a-zA-Z]*i|--in-place)`),
	regexp.MustCompile(`(?m)(?:^|&&|\|\||;|\|)\s*perl\s+(?:-[a-zA-Z]*i)`),
}

var (
	envAssignmentTokenRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=.*$`)
	flagTokenRe          = regexp.MustCompile(`(?:^|\s)--?[A-Za-z0-9]`)
	pathLikeTokenRe      = regexp.MustCompile(`(?:^|\s)(?:\.{1,2}/|~/|/|[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+|[A-Za-z0-9_.-]+\.[A-Za-z0-9]{1,8})(?:\s|$|[;&|<>])`)
)

type bashSalvageProbe interface {
	commandExists(context.Context, string) bool
	pathExecutable(context.Context, string) bool
}

// classify inspects an agent emit and returns the action class plus an
// auxiliary string (bash stderr for classifyInvalidBash; rewritten bash for
// classifyExec when natural-language first-line rewrite applies; "" otherwise).
//
// Classification order: empty → exit → banned → tool-call → :md →
// natural-language → bash-syntax → exec.
// Empty short-circuits before exit so `   ` doesn't accidentally pass the
// trim+exitRe check; banned runs before bash-syntax so we never invoke
// `bash -n` on a refused pattern; tool-call and natural-language both run before
// bash-syntax so obviously wrong non-bash shapes get dedicated corrections
// instead of generic shell errors.
func classify(ctx context.Context, emit string) (cls classifyResult, aux string) {
	return classifyWithSalvageProbe(ctx, emit, nil)
}

func classifyWithSalvageProbe(ctx context.Context, emit string, probe bashSalvageProbe) (cls classifyResult, aux string) {
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
	if containsToolCallPattern(emit) {
		return classifyToolCall, ""
	}
	if strings.HasPrefix(trimmed, MdPrefix) {
		// :md at the very start of the emit (bare or followed by ->agent).
		// Must fire before natural-language coercion so explicit :md keeps
		// its explicit lifecycle marker semantics.
		// Require space, tab, or newline after :md to avoid matching
		// commands like `:mdata` or `:md5sum`. The trimmed emit may start
		// with `:md`, `:md ->agent`, or `:md\nbody` — all valid forms.
		rest := strings.TrimPrefix(trimmed, MdPrefix)
		if rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' {
			if mdTrailingContinueRe.MatchString(trimmed) {
				return classifyMdContinue, ""
			}
			return classifyMdExit, ""
		}
	}
	if rewritten, ok := rewriteNaturalLanguageFirstLineBash(ctx, emit, probe); ok {
		return classifyExec, rewritten
	}
	if isNaturalLanguageEmit(emit) {
		return classifyNaturalLanguage, ""
	}
	if err := bashSyntaxCheck(ctx, emit); err != "" {
		return classifyInvalidBash, err
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

func containsToolCallPattern(emit string) bool {
	return toolCallXMLRe.MatchString(emit) || toolCallBracketRe.MatchString(emit)
}

// isNaturalLanguageEmit implements the natural-language auto-md heuristic.
// Markdown headings that start with two or more `#` are communication.
// Otherwise, the first non-whitespace byte must not be a lowercase English
// letter or `#`.
// If that byte is uppercase English, the first line must not contain `=` so
// assignment-like command forms such as `Output=$(pwd)` can still execute.
func isNaturalLanguageEmit(emit string) bool {
	trimmed := strings.TrimLeft(emit, " \t\r\n")
	if trimmed == "" {
		return false
	}
	if isMarkdownHeadingFirstLine(trimmed) {
		return true
	}

	first := trimmed[0]
	if first == '#' || (first >= 'a' && first <= 'z') {
		return false
	}

	firstLine, _, _ := strings.Cut(trimmed, "\n")
	if first >= 'A' && first <= 'Z' && strings.Contains(firstLine, "=") {
		return false
	}

	return true
}

func rewriteNaturalLanguageFirstLineBash(ctx context.Context, emit string, probe bashSalvageProbe) (string, bool) {
	if probe == nil {
		return "", false
	}
	trimmed := strings.TrimLeft(emit, " \t\r\n")
	firstLine, rest, found := strings.Cut(trimmed, "\n")
	if !found || strings.TrimSpace(rest) == "" {
		return "", false
	}
	if isMarkdownHeadingFirstLine(firstLine) {
		return "", false
	}
	if !isNaturalLanguageEmit(firstLine) {
		return "", false
	}

	rest = strings.TrimLeft(rest, "\n")
	restTrimmed := strings.TrimSpace(rest)
	if restTrimmed == "" || exitRe.MatchString(restTrimmed) {
		return "", false
	}
	if strings.HasPrefix(restTrimmed, MdPrefix) ||
		strings.HasPrefix(restTrimmed, ":continue") ||
		strings.HasPrefix(restTrimmed, ":exit") {
		return "", false
	}
	if containsBlockedPattern(rest) || containsToolCallPattern(rest) {
		return "", false
	}
	if err := bashSyntaxCheck(ctx, rest); err != "" {
		return "", false
	}
	if !shouldSalvageBashRest(ctx, rest, probe) {
		return "", false
	}

	return "# " + strings.TrimSpace(firstLine) + "\n" + rest, true
}

func isMarkdownHeadingFirstLine(emit string) bool {
	trimmed := strings.TrimLeft(emit, " \t")
	return strings.HasPrefix(trimmed, "##")
}

func shouldSalvageBashRest(ctx context.Context, emit string, probe bashSalvageProbe) bool {
	lines := effectiveShellLines(emit)
	if len(lines) == 0 {
		return false
	}

	line := lines[0]
	word, hadAssignment := firstCommandWord(line)
	if word == "" {
		return false
	}
	if isPathCommandWord(word) {
		return probe.pathExecutable(ctx, word)
	}
	if !startsLowerASCII(word) || !hasCommandEvidence(line, hadAssignment) {
		return false
	}
	return probe.commandExists(ctx, word)
}

func effectiveShellLines(emit string) []string {
	var lines []string
	for _, line := range strings.Split(emit, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}

func firstCommandWord(line string) (string, bool) {
	hadAssignment := false
	for _, token := range strings.Fields(line) {
		token = cleanProbeToken(token)
		if token == "" {
			continue
		}
		if envAssignmentTokenRe.MatchString(token) {
			hadAssignment = true
			continue
		}
		return token, hadAssignment
	}
	return "", hadAssignment
}

func cleanProbeToken(token string) string {
	token = strings.Trim(token, `"'`)
	token = strings.TrimRight(token, ";|&<>")
	if strings.ContainsAny(token, "$`\\") {
		return ""
	}
	return token
}

func isPathCommandWord(word string) bool {
	return strings.HasPrefix(word, "./") ||
		strings.HasPrefix(word, "../") ||
		strings.HasPrefix(word, "/") ||
		strings.HasPrefix(word, "~/")
}

func startsLowerASCII(word string) bool {
	if word == "" {
		return false
	}
	first := word[0]
	return first >= 'a' && first <= 'z'
}

func hasCommandEvidence(line string, hadAssignment bool) bool {
	return hadAssignment ||
		strings.Contains(line, "&&") ||
		strings.Contains(line, "||") ||
		strings.ContainsAny(line, "|;<>") ||
		flagTokenRe.MatchString(line) ||
		pathLikeTokenRe.MatchString(line)
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
