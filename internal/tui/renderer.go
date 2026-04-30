package tui

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter holds the parsed YAML header of a session .md.
type Frontmatter struct {
	SessionID string `yaml:"session_id"`
	Agent     string `yaml:"agent"`
	Model     string `yaml:"model"`
	StartedAt string `yaml:"started_at"`
}

// TurnAnchor identifies one user-message boundary in the rendered transcript.
type TurnAnchor struct {
	Number     int    // 1-indexed
	HeaderText string // first line after **λ**, no markdown formatting
	StartLine  int    // terminal-row index in the rendered output where this turn begins
}

type Rendered struct {
	Lines               []string     // one terminal row per index
	Anchors             []TurnAnchor // sorted by StartLine
	TurnEndCount        int          // count of *(turn ended)* markers
	UnfinishedBashCount int          // bash blocks with no following trailer
	Frontmatter         Frontmatter  // parsed YAML header
}

// ParseFrontmatter extracts the YAML frontmatter and returns the markdown body.
// If the .md doesn't start with "---\n", returns empty Frontmatter and the
// original input.
func ParseFrontmatter(md []byte) (Frontmatter, []byte, error) {
	hasLF := bytes.HasPrefix(md, []byte("---\n"))
	hasCRLF := bytes.HasPrefix(md, []byte("---\r\n"))
	if !hasLF && !hasCRLF {
		return Frontmatter{}, md, nil
	}

	rest := md[3:] // everything after the opening "---\n" or "---\r\n"
	var yamlBytes, body []byte

	if hasCRLF {
		// Find the closing ---\r\n in the rest
		endIdx := bytes.Index(rest, []byte("\r\n---\r\n"))
		if endIdx < 0 {
			return Frontmatter{}, md, nil
		}
		yamlBytes = rest[:endIdx] // between opening ---\r\n and closing \r\n---
		body = rest[endIdx+5:]    // after closing \r\n---
		// Strip leading \r\n left by the closing marker
		if len(body) >= 2 && body[0] == '\r' && body[1] == '\n' {
			body = body[2:]
		}
	} else {
		// LF-only
		endIdx := bytes.Index(rest, []byte("\n---\n"))
		if endIdx < 0 {
			return Frontmatter{}, md, nil
		}
		yamlBytes = rest[:endIdx]
		body = rest[endIdx+4:]
		// Strip leading \n left by the closing marker
		if len(body) > 0 && body[0] == '\n' {
			body = body[1:]
		}
	}

	var fm Frontmatter
	if err := yaml.Unmarshal(yamlBytes, &fm); err != nil {
		return Frontmatter{}, md, err
	}
	return fm, body, nil
}

// renderTurnAnchors scans the markdown body for **λ** lines and returns the
// header text for each turn.
func renderTurnAnchors(body []byte) []string {
	headers := []string{}
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "**λ** ") {
			headers = append(headers, strings.TrimPrefix(line, "**λ** "))
		}
	}
	return headers
}

// Render runs the markdown body through Glamour and produces a Rendered struct.
// Width is the terminal width used for word-wrapping.
func Render(md []byte, width int) (Rendered, error) {
	fm, body, err := ParseFrontmatter(md)
	if err != nil {
		return Rendered{}, err
	}

	renderer, err := MarkdownRenderer(width)
	if err != nil {
		return Rendered{}, err
	}

	rendered, err := renderer.Render(string(body))
	if err != nil {
		return Rendered{}, err
	}

	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")

	// Segment turns by scanning the markdown source for **λ** lines.
	turnHeaders := renderTurnAnchors(body)
	turnEndCount := strings.Count(string(body), "*(turn ended)*")

	// Count unfinished bash blocks (```bash with no following trailer).
	unfinishedBash := countUnfinishedBash(string(body))

	anchors := make([]TurnAnchor, 0, len(turnHeaders))

	for i, header := range turnHeaders {
		// Find the position of this **λ** line in the raw markdown.
		prefix := "**λ** " + header
		idx := strings.Index(string(body), prefix)
		if idx < 0 {
			continue
		}

		// Count newlines in the Glamour-rendered output before this marker.
		// We re-render up to each marker point to get accurate line counts.
		markerPoint := string(body[:idx])
		segment, err := renderer.Render(markerPoint)
		if err != nil {
			continue
		}
		startLine := strings.Count(strings.TrimRight(segment, "\n"), "\n")

		anchors = append(anchors, TurnAnchor{
			Number:     i + 1,
			HeaderText: header,
			StartLine:  startLine,
		})
	}

	return Rendered{
		Lines:               lines,
		Anchors:             anchors,
		TurnEndCount:        turnEndCount,
		UnfinishedBashCount: unfinishedBash,
		Frontmatter:         fm,
	}, nil
}

// countUnfinishedBash returns the number of ```bash blocks in body that have
// no following *[HH:MM:SS, Xs]* trailer or *(turn ended)* marker.
func countUnfinishedBash(body string) int {
	count := 0
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "```bash") && !strings.HasPrefix(trimmed, "``` bash") {
			continue
		}
		// Found a bash block; scan ahead to see if it ends with a trailer.
		count++
	}
	return count
}
