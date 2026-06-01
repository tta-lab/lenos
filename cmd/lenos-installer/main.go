// Package main provides the lenos-installer: an interactive terminal
// installer for Lenos and its ecosystem tools (temenos, organon, einai, ttal).
package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/pkg/browser"
)

var (
	githubOrg = "tta-lab"

	tools = []tool{
		{Name: "Lenos", Repo: "lenos", Binary: "lenos", ConfigKind: "json"},
		{Name: "Temenos", Repo: "temenos", Binary: "temenos", ConfigKind: "toml"},
		{Name: "Organon", Repo: "organon", Binary: "organon", ConfigKind: "toml"},
		{Name: "Einai", Repo: "einai", Binary: "einai", ConfigKind: "toml"},
		{Name: "TTAL", Repo: "ttal-cli", Binary: "ttal", ConfigKind: "toml"},
	}
)

type tool struct {
	Name       string
	Repo       string
	Binary     string
	ConfigKind string
}

type model struct {
	spinner   spinner.Model
	step      int
	err       error
	done      bool
	binDir    string
	configDir string
	installed []string
	statusMsg string
}

func (m model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case installedMsg:
		m.installed = append(m.installed, msg.name)
		m.step++
		if m.step >= len(tools) {
			m.statusMsg = "All tools installed."
			m.done = true
			m.openWebsite()
			return m, tea.Quit
		}
		m.statusMsg = fmt.Sprintf("Installing %s...", tools[m.step].Name)
		return m, installToolCmd(m.binDir, tools[m.step])
	case errMsg:
		m.err = msg.err
		return m, tea.Quit
	}
	return m, nil
}

func (m model) View() tea.View {
	var b strings.Builder
	b.WriteString("\n  Lenos Installer\n\n")

	if m.err != nil {
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff5555")).
			Render(fmt.Sprintf("  Error: %v\n", m.err)))
		return tea.NewView(b.String())
	}

	for _, name := range m.installed {
		fmt.Fprintf(&b, "  %s %s\n",
			lipgloss.NewStyle().
				Foreground(lipgloss.Color("#50fa7b")).
				Render("✓"),
			name,
		)
	}

	if !m.done {
		fmt.Fprintf(&b, "  %s %s\n", m.spinner.View(), m.statusMsg)
	} else {
		fmt.Fprintf(&b, "\n  %s\n",
			lipgloss.NewStyle().
				Foreground(lipgloss.Color("#f1fa8c")).
				Render("Done. Run `lenos` to get started."),
		)
	}

	return tea.NewView(b.String())
}

func (m *model) openWebsite() {
	_ = browser.OpenURL("https://lenos.sh")
}

func (m *model) run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot find home dir: %w", err)
	}

	m.binDir = filepath.Join(home, ".local", "bin")
	m.configDir = filepath.Join(home, ".config", "ttal")

	if err := os.MkdirAll(m.binDir, 0o755); err != nil {
		return fmt.Errorf("cannot create bin dir: %w", err)
	}
	if err := os.MkdirAll(m.configDir, 0o755); err != nil {
		return fmt.Errorf("cannot create config dir: %w", err)
	}

	m.statusMsg = fmt.Sprintf("Installing %s...", tools[0].Name)

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		return err
	}

	if m.err != nil {
		return m.err
	}

	return nil
}

type (
	installedMsg struct{ name string }
	errMsg       struct{ err error }
)

func installToolCmd(binDir string, t tool) tea.Cmd {
	return func() tea.Msg {
		if err := installTool(binDir, t); err != nil {
			return errMsg{err}
		}
		return installedMsg{name: t.Name}
	}
}

func installTool(binDir string, t tool) error {
	downloadURL := fmt.Sprintf(
		"https://github.com/%s/%s/releases/latest/download/%s_%s_%s.tar.gz",
		githubOrg, t.Repo, t.Binary, titleOS(), arch(),
	)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("build request %s: %w", t.Name, err)
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", t.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%s not found for %s/%s (release may not exist yet)",
			t.Name, runtime.GOOS, runtime.GOARCH)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", t.Name, resp.StatusCode)
	}

	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("gunzip %s: %w", t.Name, err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("untar %s: %w", t.Name, err)
		}

		base := filepath.Base(hdr.Name)
		if base != t.Binary {
			continue
		}

		dst := filepath.Join(binDir, t.Binary)
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return fmt.Errorf("write %s: %w", t.Name, err)
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return fmt.Errorf("extract %s: %w", t.Name, err)
		}
		f.Close()
		return nil
	}

	return fmt.Errorf("binary %s not found in archive", t.Binary)
}

func titleOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "Darwin"
	case "linux":
		return "Linux"
	default:
		return runtime.GOOS
	}
}

func arch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	default:
		return runtime.GOARCH
	}
}

var httpClient = func() *http.Client {
	return &http.Client{Timeout: 60 * time.Second}
}

func main() {
	m := &model{
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}

	if err := m.run(); err != nil {
		fmt.Fprintf(os.Stderr, "Install failed: %v\n", err)
		os.Exit(1)
	}

	home, _ := os.UserHomeDir()
	binDir := filepath.Join(home, ".local", "bin")
	fmt.Printf("\nAdd %s to your PATH if it's not already:\n", binDir)
	fmt.Printf("  export PATH=\"%s:$PATH\"\n\n", binDir)

	writeDefaultConfigs()
}

func writeDefaultConfigs() {
	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, ".config", "ttal")
	os.MkdirAll(configDir, 0o755)

	ttalCfg := `shell = "fish"

references_path = "~/code/references"

[flicknote]
  inline_projects = ["plan", "fix", "orientation"]

[sync]
  worker_agent_paths = []
  global_prompt_path = ""
  rules_paths = []
  skills_paths = []

[voice]
  vocabulary = ["session", "claude", "codex", "worker", "tmux", "taskwarrior", "flicknote"]

[teams.default]
  team_path = ""
  data_dir = "~/.ttal"
  taskrc = "~/.taskrc"
  voice_language = "en"
  default_runtime = "lenos"
  emoji_reactions = false

[kubernetes]
  allowed_namespaces = ["default"]

mcp_port = 9783
allow_env = [
  "TTAL_*",
  "LENOS_*",
  "TMUX",
  "TMUX_*",
  "GOROOT",
  "GOPATH",
  "GOBIN",
  "CGO_ENABLED",
  "CARGO_HOME",
  "RUSTUP_HOME",
]

allow_write = [
  "~/.einai",
  "~/.ttal",
  "~/.task",
  "~/.config/ttal",
  "~/.diary",
  "~/.local/share/flicknote",
  "/tmp",
  "~/go/pkg",
  "~/.cache/go/build",
  "~/.cache/golangci-lint",
  "~/.cache/cargo",
  "~/.cargo/registry",
  "~/.cargo/git",
  "~/.cache/organon",
  "~/.npm/_logs",
  "~/.claude",
  "~/.agents",
]

allow_read = [
  "~/.mmx",
  "~/.config/diary",
  "~/.config/temenos",
  "~/.config/flicknote",
  "~/.config/git",
  "~/.gitconfig",
  "~/.taskrc",
  "~/.cargo/config.toml",
  "~/.rustup",
  "~/code/projects",
  "~/code/references",
]

default_runtime = "lenos"
agents_paths = []
references_path = "~/code/references"
`

	writeIfNew(filepath.Join(configDir, "config.toml"), ttalCfg)
	writeIfNew(filepath.Join(configDir, "projects.toml"), defaultProjectsTOML())
	writeIfNew(filepath.Join(configDir, "humans.toml"), defaultHumansTOML())
	writeIfNew(filepath.Join(configDir, "roles.toml"), defaultRolesTOML())
	writeIfNew(filepath.Join(configDir, "sandbox.toml"), defaultSandboxTOML())
	writeIfNew(filepath.Join(configDir, "pipelines.toml"), defaultPipelinesTOML())
	writeIfNew(filepath.Join(configDir, "prompts.toml"), defaultPromptsTOML())

	// Lenos config.json.
	lenosDir := filepath.Join(home, ".lenos")
	os.MkdirAll(lenosDir, 0o755)

	lenosCfg := map[string]any{
		"hooks": map[string]any{
			"post_step": "ttal status update",
		},
		"options": map[string]any{
			"disable_notifications":        true,
			"disable_provider_auto_update": true,
			"message_block_prefill":        true,
			"tui": map[string]any{
				"show_thinking": false,
			},
			"context_paths": []string{filepath.Join(home, ".claude", "CLAUDE.md")},
			"agent_paths":   []string{},
		},
	}
	cfgPath := filepath.Join(lenosDir, "config.json")
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		data, _ := json.MarshalIndent(lenosCfg, "", "  ")
		os.WriteFile(cfgPath, data, 0o644)
	}
}

func writeIfNew(path, content string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		os.WriteFile(path, []byte(content), 0o644)
	}
}

func defaultProjectsTOML() string {
	return `# TTAL Project Registry
# Maps short aliases to repo paths. Used by ` + "`ttal jump <alias>`" + `.
#
# Format:
#   [alias]
#   name = "Full Name"
#   path = "/absolute/path"
#   tags = ["tag1", "tag2"]
`
}

func defaultHumansTOML() string {
	return `# Human contact registry. Used by ` + "`ttal send`" + `.
#
# Format:
#   [handle]
#   name = "Full Name"
#   telegram = "@username"
`
}

func defaultRolesTOML() string {
	return `# Role definitions for agents.
`
}

func defaultSandboxTOML() string {
	return `# Sandbox configuration for temenos.
# Default: no overrides — temenos uses its own config.
`
}

func defaultPipelinesTOML() string {
	return `# Pipeline definitions for ttal task orchestration.
`
}

func defaultPromptsTOML() string {
	return `# Custom prompt templates.
`
}
