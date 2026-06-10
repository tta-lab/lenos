// Package main provides the lenos-installer: an interactive terminal
// installer for Lenos and its ecosystem tools (temenos, organon, einai).
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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/pkg/browser"
)

const websiteURL = "https://tta-lab.github.io/lenos-website/"

var (
	githubOrg = "tta-lab"

	tools = []tool{
		{Name: "Lenos", Repo: "lenos", Binary: "lenos", ConfigKind: "json"},
		{Name: "Organon", Repo: "organon", Binary: "organon", Binaries: []string{"src", "web", "skill", "project"}, ConfigKind: "toml"},
		{Name: "Einai", Repo: "einai", Binary: "ei", UseReleaseAPI: true},
	}
)

type tool struct {
	Name       string
	Repo       string
	Binary     string
	Binaries   []string // multiple binaries to extract from one archive (e.g. organon)
	ConfigKind string
	// DownloadName overrides the archive filename for the download URL.
	// Default is "{Binary}_{titleOS}_{arch}.tar.gz".
	DownloadName string
	// UseReleaseAPI fetches the download URL from the GitHub releases API
	// instead of the /releases/latest/download/ direct link. Use this for
	// release archives that include version in the filename.
	UseReleaseAPI bool
}

type model struct {
	spinner   spinner.Model
	step      int
	err       error
	done      bool
	binDir    string
	installed []string
	statusMsg string
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, installToolCmd(m.binDir, tools[0]))
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
		fmt.Fprintf(&b, "  %s\n",
			lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ff5555")).
				Render(fmt.Sprintf("Error: %v", m.err)))
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
	_ = browser.OpenURL(websiteURL)
}

func (m *model) run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot find home dir: %w", err)
	}

	m.binDir = filepath.Join(home, ".local", "bin")

	if err := os.MkdirAll(m.binDir, 0o755); err != nil {
		return fmt.Errorf("cannot create bin dir: %w", err)
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
	var downloadURL string
	if t.UseReleaseAPI {
		u, err := releaseAssetURL(t)
		if err != nil {
			return err
		}
		downloadURL = u
	} else {
		filename := t.DownloadName
		if filename == "" {
			filename = fmt.Sprintf("%s_%s_%s.tar.gz", t.Binary, titleOS(), arch())
		}
		downloadURL = fmt.Sprintf(
			"https://github.com/%s/%s/releases/latest/download/%s",
			githubOrg, t.Repo, filename,
		)
	}

	req, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, downloadURL, nil,
	)
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

	// Determine which binaries to extract.
	want := t.Binaries
	if len(want) == 0 {
		want = []string{t.Binary}
	}
	wantSet := make(map[string]bool, len(want))
	for _, b := range want {
		wantSet[b] = true
	}

	tr := tar.NewReader(gzr)
	found := make(map[string]bool)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("untar %s: %w", t.Name, err)
		}

		base := filepath.Base(hdr.Name)
		if !wantSet[base] {
			continue
		}

		dst := filepath.Join(binDir, base)
		part := dst + ".part"
		f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return fmt.Errorf("write %s: %w", t.Name, err)
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			os.Remove(part)
			return fmt.Errorf("extract %s: %w", t.Name, err)
		}
		f.Close()
		if err := os.Rename(part, dst); err != nil {
			os.Remove(part)
			return fmt.Errorf("install %s: %w", t.Name, err)
		}
		found[base] = true

		if len(found) == len(want) {
			return nil
		}
	}

	for _, b := range want {
		if !found[b] {
			return fmt.Errorf("binary %s not found in %s archive", b, t.Name)
		}
	}
	return nil
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
	case "arm64":
		return "arm64"
	default:
		return runtime.GOARCH
	}
}

var httpClient = func() *http.Client {
	return &http.Client{Timeout: 120 * time.Second}
}

func releaseAssetURL(t tool) (string, error) {
	apiURL := fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/releases/latest",
		githubOrg, t.Repo,
	)
	req, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, apiURL, nil,
	)
	if err != nil {
		return "", fmt.Errorf("release API %s: %w", t.Name, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("release API %s: %w", t.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release API %s: HTTP %d", t.Name, resp.StatusCode)
	}
	var release struct {
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("release API %s: %w", t.Name, err)
	}
	// Find the asset for this OS/arch.
	want := fmt.Sprintf("%s_%s", titleOS(), arch())
	for _, a := range release.Assets {
		if strings.Contains(a.Name, want) && strings.HasSuffix(a.Name, ".tar.gz") {
			return a.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("%s: no release asset found for %s/%s",
		t.Name, titleOS(), arch())
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

	installDefuddle()

	writeDefaultConfigs()
}

func installDefuddle() {
	if _, err := exec.LookPath("npm"); err != nil {
		fmt.Println("  ⚠ npm is not installed. The web fetch tool requires defuddle.")
		fmt.Println("    Install Node.js (https://nodejs.org) then run: npm install -g defuddle")
		fmt.Println("    Defuddle sanitizes web content, removes ads and tracking, and")
		fmt.Println("    extracts clean article text — far more reliable than raw HTTP.")
		fmt.Println()
		return
	}

	fmt.Println("  Installing defuddle (web content sanitizer)...")
	npmInstall := exec.CommandContext(context.Background(), "npm", "install", "-g", "defuddle")
	npmInstall.Stdout = os.Stdout
	npmInstall.Stderr = os.Stderr
	if err := npmInstall.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ Failed to install defuddle: %v\n", err)
		fmt.Println("  Web fetch will still work when defuddle is available on PATH.")
	} else {
		fmt.Println("  ✓ defuddle installed.")
	}
	fmt.Println()
}

func writeDefaultConfigs() {
	home, _ := os.UserHomeDir()

	// Temenos config.
	temeDir := filepath.Join(home, ".config", "temenos")
	if err := os.MkdirAll(temeDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ Cannot create %s: %v\n", temeDir, err)
	} else {
		writeIfNew(filepath.Join(temeDir, "config.toml"), temenosConfigTOML())
	}

	// Lenos config.json.
	lenosDir := filepath.Join(home, ".lenos")
	if err := os.MkdirAll(lenosDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ Cannot create %s: %v\n", lenosDir, err)
		return
	}

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
		data, err := json.MarshalIndent(lenosCfg, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ Failed to generate lenos config: %v\n", err)
			return
		}
		os.WriteFile(cfgPath, data, 0o644)
	}
}

func writeIfNew(path, content string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		os.WriteFile(path, []byte(content), 0o644)
	}
}

func temenosConfigTOML() string {
	return `# Temenos sandbox configuration.
# Lenos links the temenos sandbox SDK directly (no daemon, no socket).
# Edit this file to change which paths the sandbox can read and write.


# Environment variables the sandbox passes through.
allow_env = [
  "LENOS_*",
  "TMUX",
  "TMUX_*",
  "GOROOT",
  "GOPATH",
  "GOBIN",
  "GOMODCACHE",
  "GOCACHE",
  "GOTOOLCHAIN",
  "GOTELEMETRY",
  "GOPROXY",
  "GOSUMDB",
  "GOPRIVATE",
  "GONOPROXY",
  "GONOSUMDB",
  "CGO_ENABLED",
  "CC",
  "CXX",
  "PKG_CONFIG_*",
  "CARGO_HOME",
  "CARGO_TARGET_DIR",
  "CARGO_BUILD_TARGET",
  "CARGO_TARGET_*",
  "RUSTUP_HOME",
  "RUSTUP_TOOLCHAIN",
  "RUSTC",
  "RUSTDOC",
  "RUSTFLAGS",
  "RUSTDOCFLAGS",
  "RUST_BACKTRACE",
  "RUST_LOG",
  "LIBCLANG_PATH",
  "BINDGEN_EXTRA_CLANG_ARGS",
  "EXA_API_KEY",
  "BRAVE_API_KEY",
  "HTTP_PROXY",
  "HTTPS_PROXY",
  "ALL_PROXY",
  "NO_PROXY",
  "http_proxy",
  "https_proxy",
  "all_proxy",
  "no_proxy",
]

# Paths the sandbox can write to.
allow_write = [
  "~/.temenos",
  "~/.lenos",
  "~/.ttal",
  "~/.task",
  "~/.diary",
  "~/.local/share/flicknote",
  "/private/var/folders",
  "~/go/pkg",
  "~/Library/Caches/go-build",
  "~/Library/Caches/golangci-lint",
  "~/Library/Caches/cargo",
  "~/.cargo/registry",
  "~/.cargo/git",
  "~/.cargo/advisory-dbs",
  "~/.cache/organon",
  "~/.npm/_logs",
  "~/.claude",
  "~/.agents",
]

# Paths the sandbox can read from.
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
  "/etc/static/ssl/certs",
  "/Library/Developer/CommandLineTools",
]
`
}
