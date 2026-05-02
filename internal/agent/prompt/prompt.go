package prompt

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/home"
	"github.com/tta-lab/lenos/internal/taskwarrior"
)

// Prompt represents a template-based prompt generator.
type Prompt struct {
	name         string
	template     string
	now          func() time.Time
	platform     string
	workingDir   string
	contextPaths []string
}

type PromptDat struct {
	Provider     string
	Model        string
	Config       config.Config
	WorkingDir   string
	IsGitRepo    bool
	Platform     string
	Date         string
	GitStatus    string
	ContextFiles []ContextFile
	JobID        string
	SkillList    string
}

type ContextFile struct {
	Path    string
	Content string
}

type Option func(*Prompt)

func WithTimeFunc(fn func() time.Time) Option {
	return func(p *Prompt) {
		p.now = fn
	}
}

func WithPlatform(platform string) Option {
	return func(p *Prompt) {
		p.platform = platform
	}
}

func WithWorkingDir(workingDir string) Option {
	return func(p *Prompt) {
		p.workingDir = workingDir
	}
}

func WithContextPaths(paths []string) Option {
	return func(p *Prompt) {
		p.contextPaths = paths
	}
}

func NewPrompt(name, promptTemplate string, opts ...Option) (*Prompt, error) {
	p := &Prompt{
		name:     name,
		template: promptTemplate,
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p, nil
}

func (p *Prompt) Build(ctx context.Context, provider, model string, store *config.ConfigStore) (string, error) {
	t, err := template.New(p.name).Parse(p.template)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}
	var sb strings.Builder
	d, err := p.promptData(ctx, provider, model, store)
	if err != nil {
		return "", err
	}
	if err := t.Execute(&sb, d); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return sb.String(), nil
}

func processFile(filePath string) *ContextFile {
	content, err := os.ReadFile(filePath)
	if err != nil {
		slog.Warn("Failed to read context file", "path", filePath, "error", err)
		return nil
	}
	return &ContextFile{
		Path:    filePath,
		Content: string(content),
	}
}

func processContextPath(p string, store *config.ConfigStore) []ContextFile {
	var contexts []ContextFile
	fullPath := p
	if !filepath.IsAbs(p) {
		fullPath = filepath.Join(store.WorkingDir(), p)
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		slog.Warn("Failed to stat context path", "path", fullPath, "error", err)
		return contexts
	}
	if info.IsDir() {
		err := filepath.WalkDir(fullPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				slog.Warn("Failed to walk context directory", "path", path, "error", err)
				return nil
			}
			if !d.IsDir() {
				if result := processFile(path); result != nil {
					contexts = append(contexts, *result)
				}
			}
			return nil
		})
		if err != nil {
			slog.Warn("Failed to walk context directory", "path", fullPath, "error", err)
		}
	} else {
		result := processFile(fullPath)
		if result != nil {
			contexts = append(contexts, *result)
		}
	}
	return contexts
}

// expandPath expands ~ and environment variables in file paths
func expandPath(path string, store *config.ConfigStore) string {
	path = home.Long(path)
	// Handle environment variable expansion using the same pattern as config
	if strings.HasPrefix(path, "$") {
		if expanded, err := store.Resolver().ResolveValue(path); err == nil {
			path = expanded
		}
	}

	return path
}

// getSkillList shells out to organon skill CLI. Binary-not-found and exit-127
// are treated as "no skills available" (silently). Other errors are logged as
// warnings. organon's own test suite covers skill discovery parsing and priority.
func getSkillList(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "skill", "list").Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "", nil
	}
	if ee := new(exec.ExitError); errors.As(err, &ee) && ee.ExitCode() == 127 {
		return "", nil
	}
	return "", err
}

func (p *Prompt) promptData(ctx context.Context, provider, model string, store *config.ConfigStore) (PromptDat, error) {
	workingDir := cmp.Or(p.workingDir, store.WorkingDir())
	platform := cmp.Or(p.platform, runtime.GOOS)

	files := map[string][]ContextFile{}

	cfg := store.Config()
	contextPaths := cfg.Options.ContextPaths
	if len(p.contextPaths) > 0 {
		contextPaths = p.contextPaths
	}
	for _, pth := range contextPaths {
		expanded := expandPath(pth, store)
		pathKey := strings.ToLower(expanded)
		if _, ok := files[pathKey]; ok {
			continue
		}
		content := processContextPath(expanded, store)
		files[pathKey] = content
	}

	skillList, err := getSkillList(ctx)
	if err != nil {
		slog.Warn("skill list unavailable", "error", err)
	}

	isGit := isGitRepo(store.WorkingDir())
	data := PromptDat{
		Provider:   provider,
		Model:      model,
		Config:     *cfg,
		WorkingDir: filepath.ToSlash(workingDir),
		IsGitRepo:  isGit,
		Platform:   platform,
		Date:       p.now().Format("1/2/2006"),
		SkillList:  skillList,
		JobID:      taskwarrior.ResolveJobIDFromCwd(),
	}
	if isGit {
		var err error
		data.GitStatus, err = getGitStatus(ctx, store.WorkingDir())
		if err != nil {
			return PromptDat{}, err
		}
	}

	for _, contextFiles := range files {
		data.ContextFiles = append(data.ContextFiles, contextFiles...)
	}
	return data, nil
}

func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func getGitStatus(ctx context.Context, dir string) (string, error) {
	branch, err := getGitBranch(ctx, dir)
	if err != nil {
		return "", err
	}
	status, err := getGitStatusSummary(ctx, dir)
	if err != nil {
		return "", err
	}
	commits, err := getGitRecentCommits(ctx, dir)
	if err != nil {
		return "", err
	}
	return branch + status + commits, nil
}

func getGitBranch(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "branch", "--show-current")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		slog.Debug("getGitBranch failed", "dir", dir, "error", err)
		return "", nil
	}
	outStr := strings.TrimSpace(string(out))
	if outStr == "" {
		return "", nil
	}
	return fmt.Sprintf("Current branch: %s\n", outStr), nil
}

func getGitStatusSummary(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--short")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		slog.Debug("getGitStatusSummary failed", "dir", dir, "error", err)
		return "", nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 20 {
		lines = lines[:20]
	}
	outStr := strings.Join(lines, "\n")
	if outStr == "" {
		return "Status: clean\n", nil
	}
	return fmt.Sprintf("Status:\n%s\n", outStr), nil
}

func getGitRecentCommits(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "log", "--oneline", "-n", "3")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		slog.Debug("getGitRecentCommits failed", "dir", dir, "error", err)
		return "", nil
	}
	if len(out) == 0 {
		return "", nil
	}
	outStr := strings.TrimSpace(string(out))
	return fmt.Sprintf("Recent commits:\n%s\n", outStr), nil
}

func (p *Prompt) Name() string {
	return p.name
}

// IsGitRepo reports whether dir is a git repository.
func IsGitRepo(dir string) bool {
	return isGitRepo(dir)
}

// GetGitStatus returns the git status for dir (branch + status + recent commits).
// Returns empty string if dir is not a git repo or on error.
func GetGitStatus(ctx context.Context, dir string) string {
	status, _ := getGitStatus(ctx, dir)
	return status
}
