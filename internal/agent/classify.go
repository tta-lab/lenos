package agent

import (
	"encoding/json"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// classifyResult enumerates how the loop should handle a model emit.
type classifyResult int

const (
	classifyExec classifyResult = iota
	classifyInvalidBash
	classifyBanned
)

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
// Classification order: banned → bash-syntax → exec.
// Banned runs before bash-syntax so refused patterns never reach the parser.
func classify(emit string) (cls classifyResult, aux string) {
	if containsBlockedPattern(emit) {
		return classifyBanned, ""
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

// bashSyntaxCheck parses the emit with mvdan's Bash parser. Returns "" on
// valid syntax, or the parser diagnostic on invalid syntax.
func bashSyntaxCheck(emit string) string {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	if _, err := parser.Parse(strings.NewReader(emit), "lenos-bash"); err != nil {
		return err.Error()
	}
	return ""
}

// isHallucinatedToolJSON returns true when the entire emit (trimmed) is a
// JSON object whose top-level keys include "function" or "tool". These are
// model hallucinations of native tool-call instructions the runtime must
// discard and retry.
func isHallucinatedToolJSON(emit string) bool {
	trimmed := strings.TrimSpace(emit)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	var top map[string]any
	if err := json.Unmarshal([]byte(trimmed), &top); err != nil {
		return false
	}
	_, hasFunction := top["function"]
	_, hasTool := top["tool"]
	return hasFunction || hasTool
}
