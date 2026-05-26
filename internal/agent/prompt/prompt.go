package prompt

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/home"
	"github.com/tta-lab/lenos/internal/protocol"
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
	identityBody string
}

type PromptDat struct {
	Provider     string
	Model        string
	Config       config.Config
	WorkingDir   string
	IsGitRepo    bool
	Platform     string
	Date         string
	IdentityBody string
	ContextFiles []ContextFile
	JobID        string
}

type ContextFile struct {
	Path    string
	Content string
}

type RuntimeContext struct {
	ContextFiles  []ContextFile
	ReadOnlyPaths []string
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

func WithIdentityBody(body string) Option {
	return func(p *Prompt) {
		p.identityBody = body
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
	t, err := template.New(p.name).Funcs(template.FuncMap{
		"narrateSection": protocol.NarrateSection,
	}).Parse(p.template)
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
	return &ContextFile{
		Path: filePath,
	}
}

func processContextPath(p string, store *config.ConfigStore) []ContextFile {
	var contexts []ContextFile
	fullPath := resolveContextPath(p, store)
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

func resolveContextPath(path string, store *config.ConfigStore) string {
	path = expandPath(path, store)
	if !filepath.IsAbs(path) {
		path = filepath.Join(store.WorkingDir(), path)
	}
	return filepath.Clean(path)
}

func LoadRuntimeContext(_ context.Context, store *config.ConfigStore, extraContextPaths []string) RuntimeContext {
	cfg := store.Config()
	contextPaths := cfg.Options.ContextPaths
	if len(extraContextPaths) > 0 {
		// Merge global and per-prompter paths, deduplicating by lowercased
		// expanded path so the same file doesn't render twice.
		seen := make(map[string]struct{}, len(contextPaths)+len(extraContextPaths))
		merged := make([]string, 0, len(contextPaths)+len(extraContextPaths))
		for _, pth := range append(contextPaths, extraContextPaths...) {
			resolved := resolveContextPath(pth, store)
			key := strings.ToLower(resolved)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, resolved)
		}
		contextPaths = merged
	}

	var out RuntimeContext
	seenFiles := map[string]struct{}{}
	for _, pth := range contextPaths {
		resolved := resolveContextPath(pth, store)
		info, err := os.Stat(resolved)
		if err != nil {
			slog.Warn("Failed to stat context path", "path", resolved, "error", err)
			continue
		}
		// Only the resolved path is added here — the sandbox's
		// AddAncestorMounts automatically adds ancestor directories as
		// MetadataOnly (stat-only, no content read) for path resolution.
		// This keeps permissions minimal: the agent can read context
		// files but parent directories are stat-only.
		out.ReadOnlyPaths = append(out.ReadOnlyPaths, resolved)
		pathKey := strings.ToLower(resolved)
		if _, ok := seenFiles[pathKey]; ok {
			continue
		}
		seenFiles[pathKey] = struct{}{}
		if info.IsDir() {
			out.ContextFiles = append(out.ContextFiles, processContextPath(resolved, store)...)
			continue
		}
		if result := processFile(resolved); result != nil {
			out.ContextFiles = append(out.ContextFiles, *result)
		}
	}
	return out
}

func (p *Prompt) promptData(_ context.Context, provider, model string, store *config.ConfigStore) (PromptDat, error) {
	workingDir := cmp.Or(p.workingDir, store.WorkingDir())
	platform := cmp.Or(p.platform, runtime.GOOS)

	cfg := store.Config()
	isGit := isGitRepo(store.WorkingDir())
	return PromptDat{
		Provider:     provider,
		Model:        model,
		Config:       *cfg,
		WorkingDir:   filepath.ToSlash(workingDir),
		IsGitRepo:    isGit,
		Platform:     platform,
		Date:         p.now().Format("1/2/2006"),
		IdentityBody: p.identityBody,
		JobID:        taskwarrior.ResolveTaskIDFromCwd(),
	}, nil
}

func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func (p *Prompt) Name() string {
	return p.name
}

// IsGitRepo reports whether dir is a git repository.
func IsGitRepo(dir string) bool {
	return isGitRepo(dir)
}
