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

// bashActionStartRe is deliberately stricter than `bash -n`: shell syntax
// accepts arbitrary command names, including prose-like CJK lines.
var bashActionStartRe = regexp.MustCompile(`^(?:[A-Za-z_][A-Za-z0-9_]*=|(?:\./|\../|/|~/)|[({]|(?:alias|awk|bash|bun|cargo|case|cat|cd|chmod|chown|command|cp|curl|date|deno|docker|echo|env|export|false|find|for|function|gh|git|go|gofmt|gofumpt|goimports|grep|head|if|jq|just|kubectl|ls|make|mkdir|mv|narrate|node|npm|perl|pnpm|printf|pwd|pytest|python|python3|rg|rm|rmdir|rsync|sed|set|sh|sleep|sort|source|src|sudo|tail|tar|task|test|touch|true|ttal|type|ulimit|uv|wc|while|which|xargs|yarn)\b)`)

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
	if rewritten, ok := rewriteNaturalLanguageFirstLineBash(ctx, emit); ok {
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

// isNaturalLanguageEmit implements the natural-language auto-md heuristic:
// the first non-whitespace byte must not be a lowercase English letter or '#'.
// If that byte is uppercase English, the first line must not contain '=' so
// assignment-like command forms such as `Output=$(pwd)` can still execute.
func isNaturalLanguageEmit(emit string) bool {
	trimmed := strings.TrimLeft(emit, " \t\r\n")
	if trimmed == "" {
		return false
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

func rewriteNaturalLanguageFirstLineBash(ctx context.Context, emit string) (string, bool) {
	trimmed := strings.TrimLeft(emit, " \t\r\n")
	firstLine, rest, found := strings.Cut(trimmed, "\n")
	if !found || strings.TrimSpace(rest) == "" {
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
	if !looksLikeBashAction(rest) {
		return "", false
	}
	if err := bashSyntaxCheck(ctx, rest); err != "" {
		return "", false
	}

	return "# " + strings.TrimSpace(firstLine) + "\n" + rest, true
}

func looksLikeBashAction(emit string) bool {
	for _, line := range strings.Split(emit, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return bashActionStartRe.MatchString(trimmed)
	}
	return false
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
