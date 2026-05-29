package lenosbash

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type renderedDiagnosticView struct {
	Kind       string
	Message    string
	Line       int
	Column     int
	Offset     int
	Incomplete bool
	SourceLine string
	Label      string
	Help       string
}

// RenderDiagnostic renders a parser diagnostic for the model-facing runtime
// repair prompt.
func RenderDiagnostic(source string, diag Diagnostic) string {
	rendered := renderedDiagnostic(source, diag)
	var b strings.Builder
	b.WriteString("[runtime] invalid Lenos Bash\n\n")
	b.WriteString("error: ")
	b.WriteString(rendered.Message)
	b.WriteString("\n")

	if rendered.Line > 0 && rendered.SourceLine != "" {
		b.WriteString("\n")
		writeSourceExcerpt(&b, source, rendered)
	}

	if rendered.Help != "" {
		b.WriteString("\nhelp: ")
		b.WriteString(rendered.Help)
		b.WriteString("\n")
		writeHelpRewrite(&b, rendered)
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderedDiagnostic(source string, diag Diagnostic) renderedDiagnosticView {
	rendered := renderedDiagnosticView{
		Kind:       diag.Kind,
		Message:    diag.Message,
		Line:       diag.Line,
		Column:     diag.Column,
		Offset:     diag.Offset,
		Incomplete: diag.Incomplete,
	}
	if rendered.Message == "" {
		rendered.Message = "invalid syntax"
	}
	rendered.SourceLine = sourceLine(source, diag.Line)
	rendered.Label = diagnosticLabel(diag)
	rendered.Help = diagnosticHelp(diag)
	return rendered
}

func writeSourceExcerpt(b *strings.Builder, source string, diag renderedDiagnosticView) {
	lines := sourceLines(source)
	if len(lines) == 0 || diag.Line < 1 || diag.Line > len(lines) {
		return
	}

	start := max(diag.Line-1, 1)
	end := min(diag.Line+1, len(lines))
	width := len(fmt.Sprintf("%d", end))
	for lineNo := start; lineNo <= end; lineNo++ {
		fmt.Fprintf(b, "  %*d | %s\n", width, lineNo, lines[lineNo-1])
		if lineNo == diag.Line {
			caretColumn := displayColumn(diag.SourceLine, diag.Column)
			if caretColumn > 1 {
				fmt.Fprintf(
					b,
					"    %s| %s^",
					strings.Repeat(" ", width-1),
					strings.Repeat(" ", caretColumn-1),
				)
			} else {
				fmt.Fprintf(b, "    %s| ^", strings.Repeat(" ", width-1))
			}
			if diag.Label != "" {
				b.WriteString(" ")
				b.WriteString(diag.Label)
			}
			b.WriteString("\n")
		}
	}
}

func writeHelpRewrite(b *strings.Builder, diag renderedDiagnosticView) {
	switch {
	case diag.Incomplete || strings.Contains(diag.Message, "closing message block"):
		b.WriteString("\n  m#####\"\n")
		b.WriteString("  Text can mention m####\"...\"#### safely.\n")
		b.WriteString("  \"#####\n")
	case strings.Contains(diag.Message, "message block"):
		b.WriteString("\n  m\"Testing now.\"\n  go test ./...\n")
	default:
		b.WriteString("\n  m\"Done.\"\n  go test ./...\n")
	}
}

func diagnosticLabel(diag Diagnostic) string {
	switch {
	case diag.Incomplete || strings.Contains(diag.Message, "closing message block"):
		return "message block starts here"
	case strings.Contains(diag.Message, "beginning of a physical line"):
		return "move `m` to its own physical line"
	case strings.Contains(diag.Message, "top level"):
		return "message blocks must be top-level"
	case strings.Contains(diag.Message, "target"):
		return "message block target is invalid"
	case diag.Kind == "shell_parse_error":
		return "bash syntax error"
	default:
		return "invalid syntax"
	}
}

func diagnosticHelp(diag Diagnostic) string {
	switch {
	case diag.Incomplete || strings.Contains(diag.Message, "closing message block"):
		return "use a matching closing delimiter. If the body contains `\"####`, wrap the outer message with more `#`"
	case strings.Contains(diag.Message, "beginning of a physical line"):
		return "put the message block on its own line"
	case strings.Contains(diag.Message, "top level"):
		return "put the message block at top level and keep control flow in bash"
	case strings.Contains(diag.Message, "target"):
		return "use a simple target name or omit the target"
	default:
		return "emit valid bash or put natural language in a message block"
	}
}

func sourceLine(source string, lineNo int) string {
	lines := sourceLines(source)
	if lineNo < 1 || lineNo > len(lines) {
		return ""
	}
	return lines[lineNo-1]
}

func sourceLines(source string) []string {
	if source == "" {
		return nil
	}
	raw := strings.SplitAfter(source, "\n")
	lines := make([]string, 0, len(raw))
	for i, line := range raw {
		if line == "" && i == len(raw)-1 {
			continue
		}
		lines = append(lines, strings.TrimRight(line, "\r\n"))
	}
	return lines
}

func displayColumn(line string, byteColumn int) int {
	if byteColumn <= 1 {
		return 1
	}
	byteOffset := min(byteColumn-1, len(line))
	display := 1
	for i := 0; i < byteOffset; {
		r, size := utf8.DecodeRuneInString(line[i:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		display++
		i += size
	}
	return display
}
