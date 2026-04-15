package agent

import (
	"context"
	_ "embed"
	"runtime"
	"time"

	"github.com/tta-lab/lenos/internal/agent/prompt"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/logos/v2"
)

//go:embed templates/coder.md.tpl
var coderPromptTmpl []byte

//go:embed templates/task.md.tpl
var taskPromptTmpl []byte

//go:embed templates/initialize.md.tpl
var initializePromptTmpl []byte

func coderPrompt(opts ...prompt.Option) (*prompt.Prompt, error) {
	systemPrompt, err := prompt.NewPrompt("coder", string(coderPromptTmpl), opts...)
	if err != nil {
		return nil, err
	}
	return systemPrompt, nil
}

func taskPrompt(opts ...prompt.Option) (*prompt.Prompt, error) {
	systemPrompt, err := prompt.NewPrompt("task", string(taskPromptTmpl), opts...)
	if err != nil {
		return nil, err
	}
	return systemPrompt, nil
}

// SystemPrompt builds the full system prompt by concatenating:
// 1. logos.BuildSystemPrompt (base: cmd block format, env, available commands)
// 2. cmd-git.tpl (git section with attribution)
// 3. coder post-template (lenos-specific: rules, style, conventions)
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

	base, err := logosBuildSystemPrompt(workingDir, cmds)
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

func logosBuildSystemPrompt(workingDir string, cmds []logos.CommandDoc) (string, error) {
	base, err := logos.BuildSystemPrompt(logos.PromptData{
		WorkingDir: workingDir,
		Platform:   runtime.GOOS,
		Date:       time.Now().UTC().Format("2006-01-02"),
		Commands:   cmds,
	})
	if err != nil {
		return "", err
	}
	return base, nil
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
