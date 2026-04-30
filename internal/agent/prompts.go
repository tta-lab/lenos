package agent

import (
	"context"
	_ "embed"
	"runtime"
	"time"

	"github.com/tta-lab/lenos/internal/agent/prompt"
	"github.com/tta-lab/lenos/internal/config"
)

//go:embed templates/coder.md.tpl
var coderPromptTmpl []byte

//go:embed templates/initialize.md.tpl
var initializePromptTmpl []byte

func coderPrompt(opts ...prompt.Option) (*prompt.Prompt, error) {
	systemPrompt, err := prompt.NewPrompt("coder", string(coderPromptTmpl), opts...)
	if err != nil {
		return nil, err
	}
	return systemPrompt, nil
}

// SystemPrompt builds the full system prompt by concatenating:
//  1. The bash-first base prompt (env, output protocol, available commands).
//  2. cmd-git.tpl (git section with attribution).
//  3. The coder post-template (lenos-specific rules, style, conventions).
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

	coder, err := buildCoderPostTemplate(ctx, provider, model, store, opts...)
	if err != nil {
		return "", err
	}

	return base + "\n" + gitSection + "\n" + coder, nil
}

func buildCoderPostTemplate(
	ctx context.Context,
	provider, model string,
	store *config.ConfigStore,
	opts ...prompt.Option,
) (string, error) {
	p, err := coderPrompt(opts...)
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
