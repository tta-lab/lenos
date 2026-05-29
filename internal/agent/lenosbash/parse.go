package lenosbash

import (
	"errors"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

type MessageBlock struct {
	Target string
	Body   string
	Line   int
	Column int
	Offset int
}

type Diagnostic struct {
	Kind       string
	Message    string
	Line       int
	Column     int
	Offset     int
	Incomplete bool
	Filename   string
}

type Parsed struct {
	Original string
	Bash     string
	Messages []MessageBlock
	HasBash  bool
}

func Parse(source string) (Parsed, *Diagnostic) {
	if diag := diagnoseNonBashShape(source); diag != nil {
		return Parsed{Original: source}, diag
	}

	blocks, clean, err := syntax.ScanMsgBlocks([]byte(source), 0)
	if err != nil {
		return Parsed{Original: source}, diagnosticFromError("message_block_error", err)
	}

	parsed := Parsed{
		Original: source,
		Bash:     compactCleanBash(string(clean)),
		Messages: make([]MessageBlock, 0, len(blocks)),
	}
	parsed.HasBash = strings.TrimSpace(parsed.Bash) != ""
	for _, block := range blocks {
		parsed.Messages = append(parsed.Messages, MessageBlock{
			Target: block.Target,
			Body:   block.Body,
			Line:   int(block.Pos().Line()),
			Column: int(block.Pos().Col()),
			Offset: int(block.Pos().Offset()),
		})
	}

	if parsed.HasBash {
		parser := syntax.NewParser()
		if _, err := parser.Parse(strings.NewReader(string(clean)), ""); err != nil {
			return parsed, diagnosticFromError("shell_parse_error", err)
		}
	}

	return parsed, nil
}

func diagnoseNonBashShape(source string) *Diagnostic {
	trimmed := strings.TrimLeft(source, " \t\r\n")
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "```") {
		return &Diagnostic{
			Kind:    "shell_parse_error",
			Message: "fenced code blocks are not valid Lenos Bash",
			Line:    1,
			Column:  1,
			Offset:  0,
		}
	}
	if first := trimmed[0]; first >= 'A' && first <= 'Z' {
		return &Diagnostic{
			Kind:    "shell_parse_error",
			Message: "raw prose is not valid Lenos Bash; use a message block",
			Line:    1,
			Column:  1,
			Offset:  0,
		}
	}
	return nil
}

func compactCleanBash(clean string) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(clean, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}

func diagnosticFromError(kind string, err error) *Diagnostic {
	var msgErr syntax.MessageBlockError
	if errors.As(err, &msgErr) {
		return &Diagnostic{
			Kind:       kind,
			Message:    msgErr.Message,
			Line:       int(msgErr.Pos.Line()),
			Column:     int(msgErr.Pos.Col()),
			Offset:     int(msgErr.Pos.Offset()),
			Incomplete: msgErr.Incomplete(),
		}
	}

	var parseErr syntax.ParseError
	if errors.As(err, &parseErr) {
		return &Diagnostic{
			Kind:       kind,
			Message:    parseErr.Text,
			Line:       int(parseErr.Pos.Line()),
			Column:     int(parseErr.Pos.Col()),
			Offset:     int(parseErr.Pos.Offset()),
			Incomplete: parseErr.Incomplete,
			Filename:   parseErr.Filename,
		}
	}

	var langErr syntax.LangError
	if errors.As(err, &langErr) {
		return &Diagnostic{
			Kind:     kind,
			Message:  langErr.Error(),
			Line:     int(langErr.Pos.Line()),
			Column:   int(langErr.Pos.Col()),
			Offset:   int(langErr.Pos.Offset()),
			Filename: langErr.Filename,
		}
	}

	return &Diagnostic{
		Kind:       kind,
		Message:    err.Error(),
		Incomplete: syntax.IsIncomplete(err),
	}
}
