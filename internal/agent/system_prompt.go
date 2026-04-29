package agent

import (
	_ "embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed templates/system_prompt.tpl
var systemPromptTemplate string

// systemPromptTmpl is parsed once at init so syntax errors surface at startup.
var systemPromptTmpl = template.Must(template.New("system").Parse(systemPromptTemplate))

// CommandDoc describes a command available to the agent. The fields mirror
// the legacy logos.CommandDoc layout so existing cmd-*.md content renders
// unchanged.
type CommandDoc struct {
	Name    string // command name, e.g. "url", "web", "src"
	Summary string // one-line description
	Help    string // full help text
}

// promptData holds the runtime context used to render the bash-first base
// system prompt.
type promptData struct {
	WorkingDir string
	Platform   string
	Date       string
	Commands   []CommandDoc
}

// buildBaseSystemPrompt renders the bash-first system prompt with runtime
// context. The result is the base prompt; SystemPrompt() appends git status
// and the lenos coder post-template.
func buildBaseSystemPrompt(d promptData) (string, error) {
	var buf strings.Builder
	if err := systemPromptTmpl.Execute(&buf, d); err != nil {
		return "", fmt.Errorf("execute system prompt template: %w", err)
	}
	return buf.String(), nil
}
