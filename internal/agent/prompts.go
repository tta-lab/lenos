package agent

import (
	"context"
	_ "embed"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/tta-lab/lenos/internal/agent/prompt"
	"github.com/tta-lab/lenos/internal/config"
)

//go:embed templates/lenos.md.tpl
var lenosWrapperTmpl []byte

//go:embed templates/coder.md
var embeddedCoderMd []byte

//go:embed templates/initialize.md.tpl
var initializePromptTmpl []byte

// SystemPrompt builds the full system prompt by concatenating:
//  1. The bash-first base prompt (env, output protocol, available commands).
//  2. cmd-git.tpl (git section with attribution).
//  3. The lenos wrapper template (universal rules + identity body + memory).
func SystemPrompt(
	ctx context.Context,
	workingDir string,
	provider, model string,
	store *config.ConfigStore,
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
		GitStatus:   prompt.GetGitStatus(ctx, workingDir),
		Attribution: store.Config().Options.Attribution.Render(),
	}
	gitSection, err := renderGitTemplate(gitData)
	if err != nil {
		return "", err
	}

	identityBody := resolveIdentityBody(store)
	wrapperOpts := append(opts, prompt.WithIdentityBody(identityBody))
	// Include AgentCoder context paths (ExtraContextFiles flow through
	// SetupAgents into AgentCoder.ContextPaths) so they render in the
	// <memory> block of lenos.md.tpl.
	if coder, ok := store.Config().Agents[config.AgentCoder]; ok && len(coder.ContextPaths) > 0 {
		wrapperOpts = append(wrapperOpts, prompt.WithContextPaths(coder.ContextPaths))
	}
	lenosWrapper, err := buildLenosWrapper(ctx, provider, model, store, wrapperOpts...)
	if err != nil {
		return "", err
	}

	return base + "\n" + gitSection + "\n" + lenosWrapper, nil
}

// resolveIdentityBody resolves the agent identity body used for the
// {{.IdentityBody}} slot in lenos.md.tpl.
//
//   - If Overrides().AgentContextFile is set (--agent flag resolved to a file),
//     reads and frontmatter-strips it.
//   - Otherwise returns the embedded coder.md as fallback.
func resolveIdentityBody(store *config.ConfigStore) string {
	agentFile := store.Overrides().AgentContextFile
	if agentFile != "" {
		data, err := os.ReadFile(agentFile)
		if err != nil {
			return stripYAMLFrontmatter(string(embeddedCoderMd))
		}
		return stripYAMLFrontmatter(string(data))
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

func InitializePrompt(cfg *config.ConfigStore) (string, error) {
	systemPrompt, err := prompt.NewPrompt("initialize", string(initializePromptTmpl))
	if err != nil {
		return "", err
	}
	return systemPrompt.Build(context.Background(), "", "", cfg)
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
