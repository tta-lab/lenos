package agent

import (
	"context"
	_ "embed"
	"os"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/tta-lab/lenos/internal/agent/lenosbash"
	"github.com/tta-lab/lenos/internal/agent/prompt"
	"github.com/tta-lab/lenos/internal/config"
)

//go:embed templates/lenos.md.tpl
var lenosWrapperTmpl []byte

//go:embed templates/coder.md
var embeddedCoderMd []byte

//go:embed templates/reviewer.md
var embeddedReviewerMd []byte

//go:embed templates/initialize.md.tpl
var initializePromptTmpl []byte

//go:embed templates/context.md
var runtimeContextPromptTmpl []byte

// SystemPrompt builds the full system prompt by concatenating:
//  1. The bash-first base prompt (env, output protocol, available commands).
//  2. cmd-git.tpl (git repo guidance with attribution).
//  3. The lenos wrapper template (universal rules + identity body + memory).
func SystemPrompt(
	ctx context.Context,
	workingDir string,
	provider, model string,
	store *config.ConfigStore,
	contextPaths []string,
	opts ...prompt.Option,
) (string, error) {
	cmds, err := loadCommandDocs()
	if err != nil {
		return "", err
	}

	base, err := buildBaseSystemPrompt(promptData{
		WorkingDir: workingDir,
		Platform:   runtime.GOOS,
		Date:       time.Now().UTC().Format("2006-01-02"),
		Commands:   cmds,
	})
	if err != nil {
		return "", err
	}

	gitData := GitTemplateData{
		IsGitRepo:   prompt.IsGitRepo(workingDir),
		Attribution: store.Config().Options.Attribution.Render(),
	}
	gitSection, err := renderGitTemplate(gitData)
	if err != nil {
		return "", err
	}

	identityBody := resolveIdentityBody(store)
	wrapperOpts := append(opts, prompt.WithIdentityBody(identityBody))
	if len(contextPaths) > 0 {
		wrapperOpts = append(wrapperOpts, prompt.WithContextPaths(contextPaths))
	}
	lenosWrapper, err := buildLenosWrapper(ctx, provider, model, store, wrapperOpts...)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n")
	if pairWith := strings.TrimSpace(store.Overrides().PairWith); pairWith != "" {
		b.WriteString("When you need to message " + pairWith + ", use the available shell command for messaging from inside a run block.\n\n")
	}
	b.WriteString(gitSection)
	b.WriteString("\n\n")
	b.WriteString(lenosWrapper)
	return b.String(), nil
}

// resolveIdentityBody resolves the agent identity body used for the
// {{.IdentityBody}} slot in lenos.md.tpl.
//
//   - If Overrides().AgentContextFile is set (--agent flag resolved to a file),
//     reads and frontmatter-strips it.
//   - Otherwise returns the embedded identity for the selected built-in agent.
func resolveIdentityBody(store *config.ConfigStore) string {
	agentFile := store.Overrides().AgentContextFile
	if agentFile != "" {
		data, err := os.ReadFile(agentFile)
		if err != nil {
			return embeddedIdentityFallback(store)
		}
		return stripYAMLFrontmatter(string(data))
	}
	return embeddedIdentityFallback(store)
}

func embeddedIdentityFallback(store *config.ConfigStore) string {
	if store.Overrides().AgentName == config.AgentReviewer {
		return stripYAMLFrontmatter(string(embeddedReviewerMd))
	}
	return stripYAMLFrontmatter(string(embeddedCoderMd))
}

func buildLenosWrapper(
	ctx context.Context,
	provider, model string,
	store *config.ConfigStore,
	opts ...prompt.Option,
) (string, error) {
	p, err := prompt.NewPrompt("lenos", string(lenosWrapperTmpl), opts...)
	if err != nil {
		return "", err
	}
	return p.Build(ctx, provider, model, store)
}

func buildRuntimeContextCommands(runtimeContext prompt.RuntimeContext) []RuntimeContextCommand {
	rendered, err := renderRuntimeContextTemplate(runtimeContext)
	if err != nil {
		return fallbackRuntimeContextCommands(runtimeContext)
	}

	sections := splitRuntimeContextSections(rendered)
	commands := make([]RuntimeContextCommand, 0, len(sections))
	for i, section := range sections {
		command := markdownBashToRunBlocks(section)
		if strings.TrimSpace(command) == "" {
			continue
		}
		commands = append(commands, RuntimeContextCommand{
			Command:  command,
			Optional: i == 0,
		})
	}
	return commands
}

func renderRuntimeContextTemplate(runtimeContext prompt.RuntimeContext) (string, error) {
	t, err := template.New("context").Funcs(template.FuncMap{
		"shellQuote": shellQuote,
	}).Parse(string(runtimeContextPromptTmpl))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, runtimeContext); err != nil {
		return "", err
	}
	return b.String(), nil
}

func splitRuntimeContextSections(rendered string) []string {
	var sections []string
	var b strings.Builder
	for _, line := range strings.Split(rendered, "\n") {
		if strings.TrimSpace(line) == "---" {
			if section := strings.TrimSpace(b.String()); section != "" {
				sections = append(sections, section)
			}
			b.Reset()
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	if section := strings.TrimSpace(b.String()); section != "" {
		sections = append(sections, section)
	}
	return sections
}

func markdownBashToRunBlocks(section string) string {
	var b strings.Builder
	inBash := false
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case !inBash && trimmed == "```bash":
			b.WriteString(lenosbash.BashStartTag)
			b.WriteString("\n")
			inBash = true
		case inBash && trimmed == "```":
			b.WriteString(lenosbash.BashEndTag)
			b.WriteString("\n")
			inBash = false
		default:
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func fallbackRuntimeContextCommands(runtimeContext prompt.RuntimeContext) []RuntimeContextCommand {
	commands := []RuntimeContextCommand{{
		Command:  lenosbash.WrapBash("List registered projects and available skills.", "project list\nskill list"),
		Optional: true,
	}}
	if len(runtimeContext.ContextFiles) > 0 {
		var readCmd strings.Builder
		readCmd.WriteString("Read key instructions.")
		readCmd.WriteString("\n\n")
		readCmd.WriteString(lenosbash.BashStartTag)
		for _, file := range runtimeContext.ContextFiles {
			readCmd.WriteString("\ncat ")
			readCmd.WriteString(shellQuote(file.Path))
		}
		readCmd.WriteString("\n")
		readCmd.WriteString(lenosbash.BashEndTag)
		commands = append(commands, RuntimeContextCommand{
			Command: readCmd.String(),
		})
	}
	commands = append(commands, RuntimeContextCommand{
		Command: "\nReady.\n\nLets rock and roll.\n",
	})
	return commands
}

func InitializePrompt(cfg *config.ConfigStore) (string, error) {
	systemPrompt, err := prompt.NewPrompt("initialize", string(initializePromptTmpl))
	if err != nil {
		return "", err
	}
	body, err := systemPrompt.Build(context.Background(), "", "", cfg)
	if err != nil {
		return "", err
	}
	return body, nil
}

// stripYAMLFrontmatter removes a single leading YAML frontmatter block
// (---\n...\n---\n) from s. Returns the body unchanged if no frontmatter
// is present or if the frontmatter is unterminated.
func stripYAMLFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		// Try terminal \n--- without trailing newline.
		if strings.HasSuffix(rest, "\n---") {
			return ""
		}
		return s // unterminated frontmatter — leave alone
	}
	return rest[end+5:]
}
