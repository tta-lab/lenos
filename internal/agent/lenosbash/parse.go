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
	if diag := diagnoseUnextractedLineStartMessage(source, blocks); diag != nil {
		return Parsed{Original: source}, diag
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

func diagnoseUnextractedLineStartMessage(source string, blocks []*syntax.MessageBlock) *Diagnostic {
	extracted := make([]sourceRange, 0, len(blocks))
	for _, block := range blocks {
		extracted = append(extracted, sourceRange{
			start: int(block.Pos().Offset()),
			end:   int(block.End().Offset()),
		})
	}

	var heredoc *heredocState
	offset := 0
	lineNo := 1
	for _, line := range strings.SplitAfter(source, "\n") {
		if heredoc != nil {
			if heredoc.matchesEnd(line) {
				heredoc = nil
			}
			offset += len(line)
			lineNo++
			continue
		}

		if diag := diagnoseUnextractedMessageInLine(source, line, lineNo, offset, extracted); diag != nil {
			return diag
		}

		if next := parseHeredocStart(line); next != nil {
			heredoc = next
		}
		offset += len(line)
		lineNo++
	}
	return nil
}

type sourceRange struct {
	start int
	end   int
}

func diagnoseUnextractedMessageInLine(source, line string, lineNo, offset int, extracted []sourceRange) *Diagnostic {
	inSingle := false
	inDouble := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return nil
			}
		case 'm':
			if inSingle || inDouble {
				continue
			}
			msgOffset := offset + i
			if inSourceRanges(msgOffset, extracted) {
				continue
			}
			block, _, err := syntax.TryParseMsgBlock([]byte(source[msgOffset:]), uint(msgOffset), uint(lineNo), uint(i+1))
			if err != nil {
				return diagnosticFromError("message_block_error", err)
			}
			if block == nil {
				continue
			}
			if countLeadingHorizontalSpace(line) == i {
				return &Diagnostic{
					Kind:    "message_block_error",
					Message: "message block is only valid at top level",
					Line:    lineNo,
					Column:  i + 1,
					Offset:  msgOffset,
				}
			}
			return &Diagnostic{
				Kind:    "message_block_error",
				Message: "message block must start at the beginning of a physical line",
				Line:    lineNo,
				Column:  i + 1,
				Offset:  msgOffset,
			}
		}
	}
	return nil
}

func inSourceRanges(offset int, ranges []sourceRange) bool {
	for _, r := range ranges {
		if offset >= r.start && offset < r.end {
			return true
		}
	}
	return false
}

type heredocState struct {
	delimiter string
	stripTabs bool
}

func (h heredocState) matchesEnd(line string) bool {
	if h.stripTabs {
		line = strings.TrimLeft(line, "\t")
	}
	line = strings.TrimRight(line, "\n")
	return line == h.delimiter
}

func parseHeredocStart(line string) *heredocState {
	idx := strings.Index(line, "<<")
	if idx < 0 {
		return nil
	}
	rest := line[idx+2:]
	stripTabs := false
	if strings.HasPrefix(rest, "-") {
		stripTabs = true
		rest = rest[1:]
	}
	rest = strings.TrimLeft(rest, " \t")
	if rest == "" {
		return nil
	}

	delimiter := heredocDelimiter(rest)
	if delimiter == "" {
		return nil
	}
	return &heredocState{delimiter: delimiter, stripTabs: stripTabs}
}

func heredocDelimiter(rest string) string {
	if rest[0] == '\'' || rest[0] == '"' {
		quote := rest[0]
		end := strings.IndexByte(rest[1:], quote)
		if end < 0 {
			return ""
		}
		return rest[1 : 1+end]
	}
	end := strings.IndexAny(rest, " \t\r\n;&|()<>")
	if end < 0 {
		return rest
	}
	return rest[:end]
}

func countLeadingHorizontalSpace(line string) int {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			return i
		}
	}
	return len(line)
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
