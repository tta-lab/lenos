package protocol

import "strings"

// NarrateSection formats body as one bash narrate heredoc.
func NarrateSection(delimiter, body string) string {
	for containsHeredocTerminator(body, delimiter) {
		delimiter += "_END"
	}

	body = strings.TrimRight(body, "\n")
	var b strings.Builder
	b.WriteString("narrate <<'")
	b.WriteString(delimiter)
	b.WriteString("'\n")
	b.WriteString(body)
	if body != "" {
		b.WriteByte('\n')
	}
	b.WriteString(delimiter)
	b.WriteByte('\n')
	return b.String()
}

func containsHeredocTerminator(body, delimiter string) bool {
	for _, line := range strings.Split(body, "\n") {
		if line == delimiter {
			return true
		}
	}
	return false
}
