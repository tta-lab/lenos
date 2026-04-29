package agent

import (
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed cmd-*.md cmd-git.tpl
var cmdFS embed.FS

// cmdDocsDirPaths is the ordered list of directories to search for cmd-*.md
// files when CMD_DOCS_DIR is unset. CMD_DOCS_DIR overrides all candidates.
var cmdDocsDirPaths = []string{
	".",
	"/app",
}

// loadCommandDocs reads cmd-*.md files from the embedded filesystem and
// returns CommandDoc entries. Filename maps to command name (cmd-note.md →
// "note"); line 1 is the summary, the rest is help text. Returns sorted by
// name (filesystem walk is deterministic).
func loadCommandDocs() ([]CommandDoc, error) {
	entries, err := cmdFS.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("read embedded cmd directory: %w", err)
	}

	var matches []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "cmd-") && strings.HasSuffix(e.Name(), ".md") {
			matches = append(matches, e.Name())
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no cmd-*.md files found in embedded filesystem")
	}

	docs := make([]CommandDoc, 0, len(matches))
	for _, name := range matches {
		doc, err := parseCmdDoc(name)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// parseCmdDoc reads a single cmd-*.md file into a CommandDoc.
// Format: line 1 = summary, remaining lines (leading blank lines trimmed) = help.
// Filename convention: cmd-<name>.md → CommandDoc.Name = "<name>".
func parseCmdDoc(name string) (CommandDoc, error) {
	raw, err := cmdFS.ReadFile(name)
	if err != nil {
		return CommandDoc{}, fmt.Errorf("read %s: %w", name, err)
	}

	cmdName := strings.TrimSuffix(strings.TrimPrefix(name, "cmd-"), ".md")
	content := string(raw)
	summary, help, _ := strings.Cut(content, "\n")
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return CommandDoc{}, fmt.Errorf("%s: first line (summary) must not be empty", name)
	}

	return CommandDoc{
		Name:    cmdName,
		Summary: summary,
		Help:    strings.TrimSpace(help),
	}, nil
}

// renderGitTemplate returns the rendered cmd-git.tpl for the given context.
func renderGitTemplate(data GitTemplateData) (string, error) {
	tpl, err := cmdFS.ReadFile("cmd-git.tpl")
	if err != nil {
		return "", fmt.Errorf("read cmd-git.tpl: %w", err)
	}

	tmpl, err := template.New("git").Parse(string(tpl))
	if err != nil {
		return "", fmt.Errorf("parse cmd-git.tpl: %w", err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute cmd-git.tpl: %w", err)
	}
	return buf.String(), nil
}

// GitTemplateData holds context for rendering cmd-git.tpl.
type GitTemplateData struct {
	IsGitRepo   bool
	GitStatus   string
	Attribution string
}
