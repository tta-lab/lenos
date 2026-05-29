package agent

import "strings"

func ttalSendCommand(to, body string) string {
	delimiter := heredocDelimiter(body)
	command := "cat <<'" + delimiter + "' | ttal send --to " + shellQuote(to) + "\n"
	if strings.HasSuffix(body, "\n") {
		return command + body + delimiter
	}
	return command + body + "\n" + delimiter
}

func heredocDelimiter(body string) string {
	base := "LENOS_MESSAGE_EOF"
	delimiter := base
	for strings.Contains(body, delimiter) {
		delimiter += "_X"
	}
	return delimiter
}
