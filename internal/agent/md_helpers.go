package agent

import "strings"

// MdPrefix is the first line prefix that identifies an :md protocol message.
// Bare `:md` sends to the session owner; `:md ->agent` sends to a named agent.
const MdPrefix = ":md"

// StripMdPrefixLine removes the first line from a block's source if it
// starts with the :md prefix. Returns the body text after the :md line.
// Returns "" for a bare `:md` line with no body. If the source doesn't start
// with :md, returns source unchanged.
func StripMdPrefixLine(source string) string {
	first, rest, found := strings.Cut(source, "\n")
	if !found {
		// Single line with only the :md prefix — empty body.
		if strings.HasPrefix(strings.TrimSpace(first), MdPrefix) {
			return ""
		}
		return source
	}
	if strings.HasPrefix(strings.TrimSpace(first), MdPrefix) {
		return strings.TrimLeft(rest, "\n")
	}
	return source
}

// ParseMdAddressee extracts the agent name after `:md ->` from the first line
// of an :md protocol message. Returns "" for bare `:md` (routed to owner),
// for text without an `:md` prefix, or for `:md ->` with no name.
//
// Used by both classifyBlock() (to confirm :md classification) and the chat
// renderer's linePrefix() (to display the addressee).
func ParseMdAddressee(text string) string {
	first := text
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		first = text[:i]
	}
	first = strings.TrimSpace(first)

	if first == MdPrefix {
		return "" // bare :md → owner
	}
	// Check for :md ->agent-name
	prefix := MdPrefix + " ->"
	if strings.HasPrefix(first, prefix) {
		after := strings.TrimPrefix(first, prefix)
		// Take only the first word (agent name)
		if i := strings.IndexAny(after, " \t"); i >= 0 {
			after = after[:i]
		}
		if after == "" {
			return ""
		}
		return after
	}
	return ""
}

// signalContext maps exit codes to signal descriptions.
// 130=SIGINT (Ctrl+C), 137=SIGKILL (killed by OOM), 143=SIGTERM.
func signalContext(code int) string {
	switch code {
	case 130:
		return "SIGINT"
	case 137:
		return "killed"
	case 143:
		return "SIGTERM"
	default:
		return ""
	}
}
