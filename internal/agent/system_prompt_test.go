package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/agent/lenosbash"
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

	assert.NotEmpty(t, got)
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

	assert.NotEmpty(t, got)
	assert.Contains(t, got, "symbol-aware source reader")
	assert.Contains(t, got, "src <file> --tree")
	assert.Contains(t, got, "web search <query>")
}

func TestBuildBaseSystemPrompt_RendersLenosBashProtocol(t *testing.T) {
	t.Parallel()

	got, err := buildBaseSystemPrompt(promptData{
		WorkingDir: "/repo",
		Platform:   "linux",
		Date:       "2026-04-29",
	})
	require.NoError(t, err)

	assert.Contains(t, got, lenosbash.BashBlock("go test ./..."))
}

func TestSystemPrompt_DoesNotTeachLegacyNarrateOrJobPolling(t *testing.T) {
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

	got, err := SystemPrompt(t.Context(), dataDir, "test-provider", "test-model", store, nil)
	require.NoError(t, err)

	assert.NotContains(t, got, "narrate")
	assert.NotContains(t, got, "temenos job list")
	assert.NotContains(t, got, "temenos job log")
	assert.NotContains(t, got, "temenos job wait")
	assert.NotContains(t, got, "telemost job list")
	assert.NotContains(t, got, "check status")
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

	assert.NotEmpty(t, got)
}

func TestSystemPrompt_GitContextDoesNotInjectStatusSnapshot(t *testing.T) {
	dataDir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dataDir
		require.NoError(t, cmd.Run())
	}

	runGit("init")
	branchName := "prompt-snapshot-branch"
	runGit("checkout", "-b", branchName)
	committedFile := "committed-snapshot-sentinel.txt"
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, committedFile), []byte("tracked"), 0o644))
	runGit("add", committedFile)
	commitMessage := "prompt snapshot sentinel commit"
	runGit(
		"-c",
		"user.name=Lenos Test",
		"-c", "user.email=lenos-test@example.com",
		"commit", "-m", commitMessage,
	)
	dirtyFile := "dirty-snapshot-sentinel.txt"
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, dirtyFile), []byte("dirty"), 0o644))

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

	assert.NotContains(t, got, branchName)
	assert.NotContains(t, got, dirtyFile)
	assert.NotContains(t, got, commitMessage)
	assert.NotEmpty(t, got)
}

func TestInitializePrompt_IsMarkdownInstruction(t *testing.T) {
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
	assert.Contains(t, got, "Clear markdown sections")
	assert.NotContains(t, got, lenosbash.BashStartTag)
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
	assert.NotEmpty(t, got)
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

	assert.Contains(t, got, "Keep this payload unchanged.")
	assert.Contains(t, got, "<external_rules>")
	assert.NotEmpty(t, got)
}

func TestSystemPrompt_PairWithDocumentsDefaultBashBlockTarget(t *testing.T) {
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

	assert.Contains(t, got, "reviewer")
	assert.Contains(t, got, "available shell command for messaging")
	assert.Contains(t, got, "bash block")
	assert.NotEmpty(t, got)
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

func TestStripYAMLFrontmatter_EmptyString(t *testing.T) {
	got := stripYAMLFrontmatter("")
	if got != "" {
		t.Errorf("stripYAMLFrontmatter('') = %q, want %q", got, "")
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
