package protocol

import "strings"

// NarrateSection formats body as one bash narrate heredoc pipeline.
func NarrateSection(delimiter, body string) string {
	for containsHeredocTerminator(body, delimiter) {
		delimiter += "_END"
	}

	body = strings.TrimRight(body, "\n")
	var b strings.Builder
	b.WriteString("cat <<'")
	b.WriteString(delimiter)
	b.WriteString("' | narrate\n")
	b.WriteString(body)
	if body != "" {
		b.WriteByte('\n')
	}
	b.WriteString(delimiter)
	b.WriteByte('\n')
	return b.String()
}

// MessageSection formats body as one Lenos Bash message block.
func MessageSection(body string) string {
	hashes := "####"
	for strings.Contains(body, `"`+hashes) {
		hashes += "#"
	}

	body = strings.TrimRight(body, "\n")
	var b strings.Builder
	b.WriteString("m")
	b.WriteString(hashes)
	b.WriteString("\"")
	if body != "" {
		b.WriteByte('\n')
		b.WriteString(body)
		b.WriteByte('\n')
	}
	b.WriteString("\"")
	b.WriteString(hashes)
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
