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

func diagnoseUnextractedLineStartMessage(source string, blocks []*syntax.MessageBlock) *Diagnostic {
	extracted := make(map[int]bool, len(blocks))
	for _, block := range blocks {
		extracted[int(block.Pos().Offset())] = true
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

		leading := countLeadingHorizontalSpace(line)
		if leading < len(line) && line[leading] == 'm' {
			msgOffset := offset + leading
			if !extracted[msgOffset] {
				block, _, err := syntax.TryParseMsgBlock([]byte(source[msgOffset:]), uint(msgOffset), uint(lineNo), uint(leading+1))
				if err != nil {
					return diagnosticFromError("message_block_error", err)
				}
				if block != nil {
					return &Diagnostic{
						Kind:    "message_block_error",
						Message: "message block is only valid at top level",
						Line:    lineNo,
						Column:  leading + 1,
						Offset:  msgOffset,
					}
				}
				if strings.HasPrefix(line[leading:], `mc"`) ||
					strings.HasPrefix(line[leading:], `mc#"`) ||
					strings.HasPrefix(line[leading:], `mc(`) {
					return &Diagnostic{
						Kind:    "message_block_error",
						Message: "mc is not a Lenos Bash message form; use m",
						Line:    lineNo,
						Column:  leading + 1,
						Offset:  msgOffset,
					}
				}
			}
		}

		if next := parseHeredocStart(line); next != nil {
			heredoc = next
		}
		offset += len(line)
		lineNo++
	}
	return nil
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
