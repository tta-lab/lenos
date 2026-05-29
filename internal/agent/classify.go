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
	classifyNaturalLanguage
)

// exitRe matches a literal `exit` / `exit N` (with optional integer and
// surrounding whitespace including tabs). Multi-line strings, exit followed
// by other commands, or "exit" inside a quoted string never match because
// classify() trims the input first and only this regex is applied.
var exitRe = regexp.MustCompile(`^\s*exit(\s+-?\d+)?\s*$`)

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
	flagTokenRe     = regexp.MustCompile(`(?:^|\s)--?[A-Za-z0-9]`)
	pathLikeTokenRe = regexp.MustCompile(`(?:^|\s)(?:\.{1,2}/|~/|/|[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+|[A-Za-z0-9_.-]+\.[A-Za-z0-9]{1,8})(?:\s|$|[;&|<>])`)
)

// classify inspects an agent emit and returns the action class plus an
// auxiliary string (bash stderr for classifyInvalidBash; "" otherwise).
//
// Classification order: empty → exit → banned → tool-call →
// natural-language → bash-syntax → exec.
// Empty short-circuits before exit so `   ` doesn't accidentally pass the
// trim+exitRe check; banned runs before bash-syntax so we never invoke
// `bash -n` on a refused pattern; tool-call and natural-language both run before
// bash-syntax so obviously wrong non-bash shapes get dedicated corrections
// instead of generic shell errors.
func classify(ctx context.Context, emit string) (cls classifyResult, aux string) {
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

func isMarkdownHeadingFirstLine(emit string) bool {
	trimmed := strings.TrimLeft(emit, " \t")
	return strings.HasPrefix(trimmed, "##")
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
