package agent

import (
	"os"
	"os/exec"
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
	assert.Contains(t, got, "narrate <<'")
	assert.Contains(t, got, "During work, write short progress notes as bash comments")
	assertValidBashSyntax(t, got)
	assert.NotContains(t, got, "Wrong shape")
	assert.NotContains(t, got, "wrong shapes")
	assert.NotContains(t, got, "common mistake")
	assert.NotContains(t, got, "rewrite it into a `narrate` heredoc")
	assert.NotContains(t, got, "narrate --to")
	assert.NotContains(t, got, "narrate --continue")
	assert.NotContains(t, got, "```")
	assert.NotContains(t, got, ":md")
	assert.NotContains(t, got, ":continue")
	assert.False(t, strings.Contains(got, ":exit"),
		"base prompt must not advertise legacy :exit")
	assert.False(t, strings.Contains(got, "Reading the README and the top-level layout.\n    cat README.md && ls"),
		"base prompt must not show prose and bash mixed in one response")

	// MUST NOT mention the legacy <cmd> markup — that's the whole point.
	assert.False(t, strings.Contains(got, "<cmd>"),
		"base prompt must not reference legacy <cmd> markup")
	assert.False(t, strings.Contains(got, "</cmd>"),
		"base prompt must not reference legacy </cmd> markup")

	// MUST NOT mention the legacy log CLI.
	assert.False(t, strings.Contains(got, "log info"),
		"base prompt must not reference legacy log info CLI")
	assert.False(t, strings.Contains(got, "log warn"),
		"base prompt must not reference legacy log warn CLI")
	assert.False(t, strings.Contains(got, "log error"),
		"base prompt must not reference legacy log error CLI")
}

func assertHeredocTerminatorsStartAtColumnZero(t *testing.T, text string) {
	t.Helper()
	count := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "EOF" {
			continue
		}
		count++
		assert.Equal(t, "EOF", line, "heredoc terminator must start at column zero")
	}
	require.NotZero(t, count, "prompt should include heredoc examples")
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
	assertValidBashSyntax(t, got)
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

	got, err := SystemPrompt(t.Context(), dataDir, "test-provider", "test-model", store, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, "You are Lenos, a powerful AI Assistant") {
		t.Errorf("default mode should contain coder identity")
	}
	assert.NotContains(t, got, "```",
		"default runtime prompt should not contain markdown fence tokens for models to copy")
	assert.NotContains(t, got, "narrate --continue",
		"default runtime prompt should not teach mid-session narration")
	assert.NotContains(t, got, "LENOS_WRAPPER")
	assert.NotContains(t, got, "<universal_rules>")
	assert.NotContains(t, got, "<critical_rules>")
	assert.NotContains(t, got, "<available_skills>")
	assert.NotContains(t, got, "<memory>")
	assertValidBashSyntax(t, got)
}

func TestSystemPrompt_GitContextDoesNotInjectStatusSnapshot(t *testing.T) {
	dataDir := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "git", "init")
	cmd.Dir = dataDir
	require.NoError(t, cmd.Run())
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "dirty.txt"), []byte("dirty"), 0o644))

	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	store, err := config.Init(dataDir, "", false)
	require.NoError(t, err)
	store.Config().Options.Attribution = &config.Attribution{}
	store.Config().Options.ContextPaths = nil

	got, err := SystemPrompt(t.Context(), dataDir, "test-provider", "test-model", store, nil)
	require.NoError(t, err)

	assert.Contains(t, got, "narrate <<'LENOS_GIT_CONTEXT'")
	assert.Contains(t, got, "Working directory is a git repository.")
	assert.Contains(t, got, "git status --short")
	assert.NotContains(t, got, "Git status (snapshot at conversation start")
	assert.NotContains(t, got, "?? dirty.txt")
	assert.NotContains(t, got, "Recent commits:")
	assertValidBashSyntax(t, got)
}

func TestInitializePrompt_IsBashNarrateScript(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	store, err := config.Init(dataDir, "", false)
	require.NoError(t, err)

	got, err := InitializePrompt(store)
	require.NoError(t, err)

	assert.Contains(t, got, "Analyze this codebase")
	assert.Contains(t, got, "narrate <<'")
	assert.NotContains(t, got, "```")
	assertValidBashSyntax(t, got)
}

func assertValidBashSyntax(t *testing.T, script string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "bash", "-n")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
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

	got, err := SystemPrompt(t.Context(), dataDir, "test-provider", "test-model", store, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, "You are a PR Review Lead") {
		t.Errorf("agent mode should contain agent body, got substring")
	}
	if strings.Contains(got, "You are Lenos, a powerful AI Assistant") {
		t.Errorf("agent mode should NOT contain coder identity when agent file given")
	}
	assert.Contains(t, got, "narrate <<'LENOS_IDENTITY_BODY'")
	assertValidBashSyntax(t, got)
}

func TestSystemPrompt_AgentMode_WrapsExternalAgentBody(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	store, err := config.Init(dataDir, "", false)
	require.NoError(t, err)
	store.Config().Options.Attribution = &config.Attribution{}
	store.Config().Options.ContextPaths = nil

	agentContent := "You are a PR Review Lead.\n\n<external_rules>\nKeep this payload unchanged.\n</external_rules>"
	agentFile := filepath.Join(dataDir, "reviewer.md")
	require.NoError(t, os.WriteFile(agentFile, []byte(agentContent), 0o644))
	store.Overrides().AgentContextFile = agentFile

	got, err := SystemPrompt(t.Context(), dataDir, "test-provider", "test-model", store, nil)
	require.NoError(t, err)

	assert.Contains(t, got, "narrate <<'LENOS_IDENTITY_BODY'")
	assert.Contains(t, got, "Keep this payload unchanged.")
	assert.Contains(t, got, "<external_rules>")
	assert.NotContains(t, got, "LENOS_WRAPPER")
	assertValidBashSyntax(t, got)
}

func TestSystemPrompt_PairWithDocumentsDefaultNarrationTarget(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	store, err := config.Init(dataDir, "", false)
	require.NoError(t, err)
	store.Config().Options.Attribution = &config.Attribution{}
	store.Config().Options.ContextPaths = nil
	store.Overrides().PairWith = "reviewer"

	got, err := SystemPrompt(t.Context(), dataDir, "test-provider", "test-model", store, nil)
	require.NoError(t, err)

	assert.Contains(t, got, "narrate <<'LENOS_NARRATION_PAIR'")
	assert.Contains(t, got, "reviewer")
	assertValidBashSyntax(t, got)
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

	got, err := SystemPrompt(t.Context(), dataDir, "test-provider", "test-model", store, nil)
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

	extraContent := "# Extra note\nThis should be in memory"
	extraFile := filepath.Join(dataDir, "extra.md")
	if err := os.WriteFile(extraFile, []byte(extraContent), 0o644); err != nil {
		t.Fatal(err)
	}
	store.Overrides().ExtraContextFiles = []string{extraFile}
	store.SetupAgents()

	cp := getCoderContextPaths(store)
	got, err := SystemPrompt(t.Context(), dataDir, "test-provider", "test-model", store, cp)
	if err != nil {
		t.Fatal(err)
	}

	assert.Contains(t, got, "narrate <<'LENOS_MEMORY'")
	assert.Contains(t, got, "narrate <<'LENOS_MEMORY_FILE_")
	assert.NotContains(t, got, "<memory>")
	assert.NotContains(t, got, "<file path=")
	if !strings.Contains(got, "Extra note") {
		t.Errorf("extra context file content should appear in output")
	}
	assertValidBashSyntax(t, got)
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

	cp := getCoderContextPaths(store)
	got, err := SystemPrompt(t.Context(), dataDir, "test-provider", "test-model", store, cp)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, "You are a reviewer") {
		t.Errorf("agent body should appear in output")
	}
	if !strings.Contains(got, "Context") {
		t.Errorf("extra context file should appear in output")
	}
	assert.Contains(t, got, "narrate <<'LENOS_MEMORY'")
	assert.NotContains(t, got, "<memory>")
	assertValidBashSyntax(t, got)
}

func TestStripYAMLFrontmatter_EmptyString(t *testing.T) {
	got := stripYAMLFrontmatter("")
	if got != "" {
		t.Errorf("stripYAMLFrontmatter('') = %q, want %q", got, "")
	}
}

func TestSystemPrompt_MultipleExtraContextFiles(t *testing.T) {
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

	extra1 := filepath.Join(dataDir, "extra1.md")
	extra2 := filepath.Join(dataDir, "extra2.md")
	if err := os.WriteFile(extra1, []byte("# Extra 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extra2, []byte("# Extra 2"), 0o644); err != nil {
		t.Fatal(err)
	}
	store.Overrides().ExtraContextFiles = []string{extra1, extra2}
	store.SetupAgents()

	cp := getCoderContextPaths(store)
	got, err := SystemPrompt(t.Context(), dataDir, "test-provider", "test-model", store, cp)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, "Extra 1") {
		t.Errorf("extra context file 1 should appear in output")
	}
	if !strings.Contains(got, "Extra 2") {
		t.Errorf("extra context file 2 should appear in output")
	}
}

func TestSystemPrompt_ZeroExtraContextFiles(t *testing.T) {
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
	store.SetupAgents()

	got, err := SystemPrompt(t.Context(), dataDir, "test-provider", "test-model", store, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, "You are Lenos, a powerful AI Assistant") {
		t.Errorf("default mode should contain coder identity even without extra context")
	}
}

func TestResolveIdentityBody_ReadErrorFallsBackToEmbedded(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	store, err := config.Init(dataDir, "", false)
	require.NoError(t, err)
	store.Config().Options.Attribution = &config.Attribution{}
	store.Config().Options.ContextPaths = nil

	// Point to a nonexistent file — should fall back to embedded coder.md.
	store.Overrides().AgentContextFile = filepath.Join(dataDir, "nonexistent.md")

	body := resolveIdentityBody(store)
	if !strings.Contains(body, "You are Lenos, a powerful AI Assistant") {
		t.Errorf("read-error fallback should contain embedded coder identity, got:\n%s", body)
	}
}
