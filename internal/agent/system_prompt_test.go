package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/config"
)

func TestBuildBaseSystemPrompt_BashFirstInvariants(t *testing.T) {
	t.Parallel()

	got, err := buildBaseSystemPrompt(promptData{
		WorkingDir: "/repo",
		Platform:   "darwin",
		Date:       "2026-04-29",
	})
	require.NoError(t, err)

	// Environment is rendered.
	assert.Contains(t, got, "Working directory: /repo")
	assert.Contains(t, got, "Platform: darwin")
	assert.Contains(t, got, "Date: 2026-04-29")

	// Bash-first protocol is described.
	assert.Contains(t, got, "raw bash")
	assert.Contains(t, got, "exit")
	assert.Contains(t, got, "narrate")
	// narrate is heredoc-only (the bash-quoting trap is the #1 cause of
	// re-prompts; one canonical form eliminates the apostrophe edge cases).
	assert.Contains(t, got, "narrate <<'EOF'",
		"narrate examples must use the heredoc form")
	assert.NotContains(t, got, `narrate "`,
		"narrate must not be advertised in double-quoted form (apostrophe trap)")
	assert.NotContains(t, got, `narrate '`,
		"narrate must not be advertised in single-quoted form (apostrophe trap)")

	// MUST NOT mention the legacy <cmd> markup — that's the whole point.
	assert.False(t, strings.Contains(got, "<cmd>"),
		"base prompt must not reference legacy <cmd> markup")
	assert.False(t, strings.Contains(got, "</cmd>"),
		"base prompt must not reference legacy </cmd> markup")

	// MUST NOT mention the legacy log CLI — narrate replaced it.
	assert.False(t, strings.Contains(got, "log info"),
		"base prompt must not reference legacy log info CLI")
	assert.False(t, strings.Contains(got, "log warn"),
		"base prompt must not reference legacy log warn CLI")
	assert.False(t, strings.Contains(got, "log error"),
		"base prompt must not reference legacy log error CLI")

	// narrate is single-mode — no severity variants.
	assert.False(t, strings.Contains(got, "narrate info"),
		"narrate is single-mode; no severity variants")
	assert.False(t, strings.Contains(got, "narrate warn"),
		"narrate is single-mode; no severity variants")
	assert.False(t, strings.Contains(got, "narrate error"),
		"narrate is single-mode; no severity variants")
}

func TestBuildBaseSystemPrompt_EmitsCommandSection(t *testing.T) {
	t.Parallel()

	got, err := buildBaseSystemPrompt(promptData{
		WorkingDir: "/repo",
		Platform:   "linux",
		Date:       "2026-04-29",
		Commands: []CommandDoc{
			{Name: "src", Summary: "symbol-aware source reader", Help: "src <file> --tree"},
			{Name: "web", Summary: "web search and fetch", Help: "web search <query>"},
		},
	})
	require.NoError(t, err)

	assert.Contains(t, got, "# Available Commands")
	assert.Contains(t, got, "## src")
	assert.Contains(t, got, "symbol-aware source reader")
	assert.Contains(t, got, "src <file> --tree")
	assert.Contains(t, got, "## web")
	assert.Contains(t, got, "web search <query>")
}

func TestStripYAMLFrontmatter_FrontmatterStripped(t *testing.T) {
	input := "---\nname: coder\nrole: worker\n---\n# Body\nContent"
	want := "# Body\nContent"
	got := stripYAMLFrontmatter(input)
	if got != want {
		t.Errorf("stripYAMLFrontmatter() = %q, want %q", got, want)
	}
}

func TestStripYAMLFrontmatter_NoFrontmatterPreserved(t *testing.T) {
	input := "# Just body\nNo frontmatter"
	got := stripYAMLFrontmatter(input)
	if got != input {
		t.Errorf("stripYAMLFrontmatter() = %q, want %q", got, input)
	}
}

func TestStripYAMLFrontmatter_UnterminatedPreserved(t *testing.T) {
	input := "---\nname: coder\n---incomplete"
	got := stripYAMLFrontmatter(input)
	if got != input {
		t.Errorf("stripYAMLFrontmatter() = %q, want %q", got, input)
	}
}

func TestStripYAMLFrontmatter_FrontmatterOnly(t *testing.T) {
	input := "---\nname: coder\n---"
	got := stripYAMLFrontmatter(input)
	if got != "" {
		t.Errorf("stripYAMLFrontmatter() = %q, want %q", got, "")
	}
}

func TestStripYAMLFrontmatter_FrontmatterWithEmptyBody(t *testing.T) {
	input := "---\nname: coder\n---\n"
	got := stripYAMLFrontmatter(input)
	if got != "" {
		t.Errorf("stripYAMLFrontmatter() = %q, want %q", got, "")
	}
}

func TestStripYAMLFrontmatter_InnerDashesNotStripped(t *testing.T) {
	input := "# Body with --- inside\nNot leading frontmatter"
	want := input
	got := stripYAMLFrontmatter(input)
	if got != want {
		t.Errorf("stripYAMLFrontmatter() = %q, want %q", got, want)
	}
}

func TestBuildBaseSystemPrompt_NoCommandSectionWhenEmpty(t *testing.T) {
	t.Parallel()

	got, err := buildBaseSystemPrompt(promptData{
		WorkingDir: "/repo",
		Platform:   "linux",
		Date:       "2026-04-29",
	})
	require.NoError(t, err)
	assert.False(t, strings.Contains(got, "# Available Commands"),
		"empty Commands slice should suppress the heading")
}

func TestSystemPrompt_DefaultMode_RendersCoderIdentity(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	store, err := config.Init(dataDir, "", false)
	if err != nil {
		t.Fatal(err)
	}
	store.Config().Options.Attribution = &config.Attribution{}
	store.Config().Options.ContextPaths = nil

	got, err := SystemPrompt(t.Context(), dataDir, "test-provider", "test-model", store)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, "You are Lenos, a powerful AI Assistant") {
		t.Errorf("default mode should contain coder identity")
	}
}

func TestSystemPrompt_AgentMode_RendersAgentBody(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	store, err := config.Init(dataDir, "", false)
	if err != nil {
		t.Fatal(err)
	}
	store.Config().Options.Attribution = &config.Attribution{}
	store.Config().Options.ContextPaths = nil

	agentContent := "You are a PR Review Lead.\n\nFocus on code quality."
	agentFile := filepath.Join(dataDir, "reviewer.md")
	if err := os.WriteFile(agentFile, []byte(agentContent), 0o644); err != nil {
		t.Fatal(err)
	}
	store.Overrides().AgentContextFile = agentFile

	got, err := SystemPrompt(t.Context(), dataDir, "test-provider", "test-model", store)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, "You are a PR Review Lead") {
		t.Errorf("agent mode should contain agent body, got substring")
	}
	if strings.Contains(got, "You are Lenos, a powerful AI Assistant") {
		t.Errorf("agent mode should NOT contain coder identity when agent file given")
	}
}

func TestSystemPrompt_AgentMode_FrontmatterStripped(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	store, err := config.Init(dataDir, "", false)
	if err != nil {
		t.Fatal(err)
	}
	store.Config().Options.Attribution = &config.Attribution{}
	store.Config().Options.ContextPaths = nil

	agentContent := "---\nname: reviewer\nrole: code-review\n---\n# Body\nActual content"
	agentFile := filepath.Join(dataDir, "reviewer.md")
	if err := os.WriteFile(agentFile, []byte(agentContent), 0o644); err != nil {
		t.Fatal(err)
	}
	store.Overrides().AgentContextFile = agentFile

	got, err := SystemPrompt(t.Context(), dataDir, "test-provider", "test-model", store)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(got, "name: reviewer") {
		t.Errorf("frontmatter should be stripped")
	}
	if !strings.Contains(got, "Actual content") {
		t.Errorf("agent body content should appear after frontmatter strip")
	}
}

func TestSystemPrompt_ExtraContextFilesStillInMemory(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	store, err := config.Init(dataDir, "", false)
	if err != nil {
		t.Fatal(err)
	}
	store.Config().Options.Attribution = &config.Attribution{}
	store.Config().Options.ContextPaths = nil

	extraContent := "# Extra note\nThis should be in <memory>"
	extraFile := filepath.Join(dataDir, "extra.md")
	if err := os.WriteFile(extraFile, []byte(extraContent), 0o644); err != nil {
		t.Fatal(err)
	}
	store.Overrides().ExtraContextFiles = []string{extraFile}
	store.SetupAgents()

	got, err := SystemPrompt(t.Context(), dataDir, "test-provider", "test-model", store)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, "<memory>") {
		t.Errorf("extra context files should be in <memory> block")
	}
	if !strings.Contains(got, "Extra note") {
		t.Errorf("extra context file content should appear in output")
	}
}

func TestSystemPrompt_AgentMode_ExtraContextFilesStillInMemory(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	store, err := config.Init(dataDir, "", false)
	if err != nil {
		t.Fatal(err)
	}
	store.Config().Options.Attribution = &config.Attribution{}
	store.Config().Options.ContextPaths = nil

	agentContent := "You are a reviewer.\n---\nbody"
	agentFile := filepath.Join(dataDir, "reviewer.md")
	if err := os.WriteFile(agentFile, []byte(agentContent), 0o644); err != nil {
		t.Fatal(err)
	}
	store.Overrides().AgentContextFile = agentFile

	extraContent := "# Context\nProject details"
	extraFile := filepath.Join(dataDir, "extra.md")
	if err := os.WriteFile(extraFile, []byte(extraContent), 0o644); err != nil {
		t.Fatal(err)
	}
	store.Overrides().ExtraContextFiles = []string{extraFile}
	store.SetupAgents()

	got, err := SystemPrompt(t.Context(), dataDir, "test-provider", "test-model", store)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, "You are a reviewer") {
		t.Errorf("agent body should appear in output")
	}
	if !strings.Contains(got, "Context") {
		t.Errorf("extra context file should appear in output")
	}
	if !strings.Contains(got, "<memory>") {
		t.Errorf("extra context files should be in <memory> block")
	}
}
