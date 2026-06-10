package agent

import (
	"context"
	_ "embed"
	"os"
	"runtime"
	"slices"
	"strconv"
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

//go:embed templates/src_context.md
var srcRuntimeContextPromptTmpl []byte

//go:embed templates/web_context.md
var webRuntimeContextPromptTmpl []byte

//go:embed templates/skill_context.md
var skillRuntimeContextPromptTmpl []byte

//go:embed templates/project_context.md
var projectRuntimeContextPromptTmpl []byte

//go:embed templates/coder_context.md
var coderRuntimeContextPromptTmpl []byte

//go:embed templates/reviewer_context.md
var reviewerRuntimeContextPromptTmpl []byte

//go:embed templates/compact_context.md
var compactRuntimeContextPromptTmpl []byte

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

type runtimeContextTemplateData struct {
	ContextFiles []prompt.ContextFile
}

type runtimeContextTemplateMeta struct {
	Order    int
	Agent    string
	Optional bool
	Content  string
}

func buildRuntimeContextCommands(runtimeContext prompt.RuntimeContext, agentName string, templates ...[]byte) []RuntimeContextCommand {
	data := runtimeContextTemplateData{
		ContextFiles: runtimeContext.ContextFiles,
	}
	rendered, err := renderRuntimeContextTemplates(data, agentName, templates...)
	if err != nil {
		return fallbackRuntimeContextCommands(runtimeContext)
	}

	commands := make([]RuntimeContextCommand, 0, len(rendered))
	for _, section := range rendered {
		command := markdownBashToRunBlocks(section.Content)
		if strings.TrimSpace(command) == "" {
			continue
		}
		commands = append(commands, RuntimeContextCommand{
			Command:  command,
			Optional: section.Optional,
		})
	}
	return commands
}

func appendCompactRuntimeContextCommand(commands []RuntimeContextCommand) []RuntimeContextCommand {
	compactCommands := buildRuntimeContextCommands(prompt.RuntimeContext{}, config.AgentCoder, compactRuntimeContextPromptTmpl)
	return append(commands, compactCommands...)
}

func renderRuntimeContextTemplates(data runtimeContextTemplateData, agentName string, templates ...[]byte) ([]runtimeContextTemplateMeta, error) {
	contexts := make([]runtimeContextTemplateMeta, 0, len(templates))
	for _, tmpl := range templates {
		rendered, err := renderRuntimeContextTemplate(data, string(tmpl))
		if err != nil {
			return nil, err
		}
		ctxTmpl := parseRuntimeContextSection(rendered)
		if ctxTmpl.Agent != "" && ctxTmpl.Agent != agentName {
			continue
		}
		if strings.TrimSpace(ctxTmpl.Content) == "" {
			continue
		}
		contexts = append(contexts, ctxTmpl)
	}
	slices.SortStableFunc(contexts, func(a, b runtimeContextTemplateMeta) int {
		return a.Order - b.Order
	})
	return contexts, nil
}

func parseRuntimeContextSection(section string) runtimeContextTemplateMeta {
	meta := runtimeContextTemplateMeta{
		Content: section,
	}
	body := meta.Content
	if !strings.HasPrefix(body, "---\n") {
		return meta
	}
	rest := body[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return meta
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "order":
			if n, err := strconv.Atoi(value); err == nil {
				meta.Order = n
			}
		case "agent":
			meta.Agent = value
		case "optional":
			meta.Optional = value == "true"
		}
	}
	meta.Content = rest[end+5:]
	return meta
}

func renderRuntimeContextTemplate(data runtimeContextTemplateData, tmpl string) (string, error) {
	t, err := template.New("context").Funcs(template.FuncMap{
		"shellQuote": shellQuote,
	}).Parse(tmpl)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

func markdownBashToRunBlocks(section string) string {
	var b strings.Builder
	inBash := false
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case !inBash && trimmed == "```bash":
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n\n") {
				b.WriteString("\n")
			}
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
	commands := []RuntimeContextCommand{
		{
			Command:  lenosbash.WrapBash("Read available source-code tool documentation.", "echo \"------src --help------\" && src --help && echo \"------src edit --help------\" && src edit --help && echo \"------src replace --help------\" && src replace --help && echo \"------src delete --help------\" && src delete --help && echo \"------src insert --help------\" && src insert --help"),
			Optional: true,
		},
		{
			Command:  lenosbash.WrapBash("Read available web tool documentation.", "echo \"------web --help------\" && web --help && echo \"------web search --help------\" && web search --help && echo \"------web fetch --help------\" && web fetch --help && echo \"------web docs --help------\" && web docs --help && echo \"------web sgraph --help------\" && web sgraph --help"),
			Optional: true,
		},
		{
			Command:  lenosbash.WrapBash("Read available skill tool documentation.", "echo \"------skill --help------\" && skill --help && echo \"------skill list------\" && skill list"),
			Optional: true,
		},
		{
			Command:  lenosbash.WrapBash("Read available project tool documentation.", "echo \"------project --help------\" && project --help && echo \"------project get orga------\" && project get orga && echo \"------project list------\" && project list"),
			Optional: true,
		},
	}
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
