package agent

import (
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// classifyResult enumerates how the loop should handle a model emit.
type classifyResult int

const (
	classifyExec classifyResult = iota
	classifyEmpty
	classifyToolCall
	classifyInvalidBash
	classifyBanned
)

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
// Classification order: empty → banned → tool-call → bash-syntax → exec.
// Banned runs before bash-syntax so refused patterns never reach the parser;
// tool-call runs before bash-syntax so obviously wrong non-bash shapes get
// dedicated corrections instead of generic shell errors.
func classify(emit string) (cls classifyResult, aux string) {
	trimmed := strings.TrimSpace(emit)
	if trimmed == "" {
		return classifyEmpty, ""
	}
	if containsBlockedPattern(emit) {
		return classifyBanned, ""
	}
	if containsToolCallPattern(emit) {
		return classifyToolCall, ""
	}
	if err := bashSyntaxCheck(emit); err != "" {
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

// bashSyntaxCheck parses the emit with mvdan's Bash parser. Returns "" on
// valid syntax, or the parser diagnostic on invalid syntax.
func bashSyntaxCheck(emit string) string {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	if _, err := parser.Parse(strings.NewReader(emit), "lenos-bash"); err != nil {
		return err.Error()
	}
	return ""
}
