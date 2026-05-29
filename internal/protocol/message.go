package protocol

import "strings"

// MessageSection formats body as one Lenos Bash message block.
func MessageSection(body string) string {
	delimiter := `####`
	for strings.Contains(body, `"`+delimiter) {
		delimiter += "#"
	}
	if strings.HasSuffix(body, "\n") {
		return "m" + delimiter + "\"\n" + body + "\"" + delimiter + "\n"
	}
	return "m" + delimiter + "\"\n" + body + "\n\"" + delimiter + "\n"
}
