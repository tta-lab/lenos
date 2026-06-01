package lenosbash

import "strings"

const (
	BashStartTag = "<run>"
	BashEndTag   = "</run>"

	ResultStartTag = "<result>"
	ResultEndTag   = "</result>"
	RuntimeTag     = "<runtime>"
	RuntimeEndTag  = "</runtime>"
)

type Parsed struct {
	Original        string
	Accepted        string
	DroppedPostBash string
	Prose           string
	Bash            []string
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

func BashBlock(command string) string {
	return BashStartTag + "\n" + strings.TrimRight(command, "\n") + "\n" + BashEndTag
}

func WrapBash(prose, command string) string {
	prose = strings.TrimRight(prose, "\n")
	if prose == "" {
		return BashBlock(command)
	}
	return prose + "\n\n" + BashBlock(command)
}

func ResultBlock(body string) string {
	return ResultStartTag + "\n" + strings.TrimRight(body, "\n") + "\n" + ResultEndTag
}

func ResultBody(block string) string {
	body := strings.TrimPrefix(block, ResultStartTag+"\n")
	return strings.TrimSuffix(body, "\n"+ResultEndTag)
}

func RuntimeBlock(body string) string {
	return RuntimeTag + "\n" + strings.TrimRight(body, "\n") + "\n" + RuntimeEndTag
}

func RuntimeLine(body string) string {
	return RuntimeTag + " " + strings.TrimSpace(body)
}

func AlertLine(body string) string {
	return RuntimeTag + " ALERT: " + strings.TrimSpace(body)
}

// Parse scans source for column-1 run tags. Text before the first run block
// is Markdown prose. Inline or indented tag text is plain text. Text after the
// first completed run block is reported as dropped tail. Nested run tags are
// treated as literal bash content so heredocs can contain tagged examples.
func Parse(source string) (Parsed, *Diagnostic) {
	if strings.TrimSpace(source) == "" {
		return Parsed{Original: source}, nil
	}
	p := &parser{src: source, line: 1, col: 1}
	return p.scan()
}

type parser struct {
	src string
	pos int

	line int
	col  int

	depth         int
	proseFrom     int
	acceptedUntil int

	prose []string
	bash  []string

	block strings.Builder
}

func (p *parser) scan() (Parsed, *Diagnostic) {
	p.proseFrom = 0
	for p.pos < len(p.src) {
		if len(p.bash) > 0 {
			p.advance(1)
			continue
		}
		switch {
		case p.atLineStart() && strings.HasPrefix(p.src[p.pos:], BashStartTag):
			p.openBash()
			p.advance(len(BashStartTag))
		case p.atLineStart() && strings.HasPrefix(p.src[p.pos:], BashEndTag):
			p.closeBash()
			p.advance(len(BashEndTag))
		default:
			if p.depth > 0 {
				p.block.WriteByte(p.src[p.pos])
			}
			p.advance(1)
		}
	}
	if p.depth > 0 {
		p.flushProse()
		body := strings.TrimPrefix(p.block.String(), "\n")
		return Parsed{
				Original:        p.src,
				Prose:           strings.Join(p.prose, "\n"),
				Bash:            []string{body},
				Accepted:        p.src,
				DroppedPostBash: "",
			}, &Diagnostic{
				Kind:       "tag_unclosed",
				Message:    "unclosed " + BashStartTag + " tag at end of response",
				Line:       p.line,
				Column:     p.col,
				Offset:     p.pos,
				Incomplete: true,
			}
	}
	p.flushProse()
	return Parsed{
		Original:        p.src,
		Accepted:        p.accepted(),
		DroppedPostBash: p.droppedPostBash(),
		Prose:           strings.Join(p.prose, "\n"),
		Bash:            p.bash,
	}, nil
}

func (p *parser) openBash() {
	if p.depth > 0 {
		p.depth++
		p.block.WriteString(BashStartTag)
		return
	}
	p.flushProse()
	p.depth = 1
	p.block.Reset()
}

func (p *parser) closeBash() {
	switch {
	case p.depth > 1:
		p.depth--
		p.block.WriteString(BashEndTag)
	case p.depth == 1:
		p.depth = 0
		body := strings.TrimPrefix(p.block.String(), "\n")
		p.bash = append(p.bash, body)
		p.acceptedUntil = p.pos + len(BashEndTag)
		p.proseFrom = len(p.src) + 1
	default:
		p.flushProse()
		p.proseFrom = p.pos + len(BashEndTag)
	}
}

func (p *parser) atLineStart() bool {
	return p.col == 1
}

func (p *parser) accepted() string {
	if p.acceptedUntil == 0 {
		return p.src
	}
	return p.src[:p.acceptedUntil]
}

func (p *parser) droppedPostBash() string {
	if p.acceptedUntil == 0 || p.acceptedUntil >= len(p.src) {
		return ""
	}
	return p.src[p.acceptedUntil:]
}

func (p *parser) flushProse() {
	if p.proseFrom < 0 || p.proseFrom > len(p.src) {
		return
	}
	text := p.src[p.proseFrom:p.pos]
	text = strings.TrimLeft(text, "\n")
	if strings.TrimSpace(text) != "" {
		p.prose = append(p.prose, text)
	}
	p.proseFrom = -1
}

func (p *parser) advance(n int) {
	for i := 0; i < n; i++ {
		if p.pos >= len(p.src) {
			return
		}
		if p.src[p.pos] == '\n' {
			p.line++
			p.col = 1
		} else {
			p.col++
		}
		p.pos++
	}
}
