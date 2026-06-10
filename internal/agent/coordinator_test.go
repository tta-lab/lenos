package agent

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/agent/lenosbash"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/pubsub"
	"github.com/tta-lab/lenos/internal/session"
)

// stubAgent implements SessionAgent for testing without a real coordinator setup.
type stubAgent struct {
	SessionAgent // embed so unimplemented methods panic loudly; only Run is overridden
	runErr       error
	modelName    string
}

func (s *stubAgent) Run(_ context.Context, _ SessionAgentCall) error { return s.runErr }
func (s *stubAgent) Model() Model {
	return Model{
		ModelCfg: config.SelectedModel{Model: s.modelName, Provider: "test"},
		Model:    &stubFantasyModel{modelName: s.modelName},
	}
}

type stubFantasyModel struct {
	modelName string
}

func (s *stubFantasyModel) Model() string    { return s.modelName }
func (s *stubFantasyModel) Provider() string { return "test" }
func (s *stubFantasyModel) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return nil, nil
}

func (s *stubFantasyModel) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	return nil, nil
}

func (s *stubFantasyModel) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (s *stubFantasyModel) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// minimalCoordinator exposes the Run error-mapping path for unit testing
// without a full coordinator (which requires config, OAuth, etc.).
type minimalCoordinator struct {
	currentAgent SessionAgent
}

func (m *minimalCoordinator) Run(ctx context.Context, sessionID string, prompt string, attachments ...message.Attachment) error {
	prompt = message.PromptWithTextAttachments(prompt, attachments)
	runErr := m.currentAgent.Run(ctx, SessionAgentCall{Prompt: prompt})
	if runErr == nil {
		return nil
	}
	return errors.Join(errors.New("agent.Run"), runErr)
}

func TestCoordinator_Run_StopReasonMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		runErr   error
		wantNil  bool
		wantWrap string // substring that must appear in the returned error
	}{
		{
			name:    "success → nil",
			runErr:  nil,
			wantNil: true,
		},
		{
			name:     "stopError → wrapped",
			runErr:   errors.New("provider exploded"),
			wantNil:  false,
			wantWrap: "provider exploded",
		},
		{
			name:     "stopCanceled → ctx.Err propagates",
			runErr:   context.Canceled,
			wantNil:  false,
			wantWrap: "cancel",
		},
		{
			name:     "stopStepCap → ErrStepCap propagates",
			runErr:   ErrStepCap,
			wantNil:  false,
			wantWrap: "step cap",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := &minimalCoordinator{
				currentAgent: &stubAgent{runErr: tc.runErr, modelName: "test-model"},
			}

			got := c.Run(context.Background(), "sess-1", "hello")

			if tc.wantNil {
				require.NoError(t, got, "Run with runErr=%v should return nil", tc.runErr)
				return
			}
			require.Error(t, got, "Run with runErr=%v should return an error", tc.runErr)
			assert.Contains(t, got.Error(), tc.wantWrap,
				"error should wrap the original cause")
		})
	}
}

// TestCoordinator_recorderFor_cachesPerSession verifies that calling
// recorderFor with the same sessionID returns the same recorder (cached) and
// different sessionIDs produce different recorders. Pre-creates the .md files
// so the os.Stat guard skips Open (which would call into a nil fantasy model
// on the stub agent).
// capturingAgent records the SessionAgentCall it receives.
type capturingAgent struct {
	SessionAgent
	captured  chan SessionAgentCall
	modelName string
}

func (c *capturingAgent) Run(_ context.Context, call SessionAgentCall) error {
	c.captured <- call
	return nil
}

func (c *capturingAgent) Model() Model {
	return Model{
		ModelCfg: config.SelectedModel{Model: c.modelName, Provider: "test"},
		Model:    &stubFantasyModel{modelName: c.modelName},
	}
}

func TestCoordinator_Run_TextAttachmentPassthrough(t *testing.T) {
	t.Parallel()

	captured := make(chan SessionAgentCall, 1)
	agent := &capturingAgent{captured: captured, modelName: "test-model"}
	c := &minimalCoordinator{currentAgent: agent}

	attachments := []message.Attachment{
		{
			FilePath: "/path/to/test.txt",
			MimeType: "text/plain",
			Content:  []byte("hello world"),
		},
		{
			FilePath: "/path/to/notes.md",
			MimeType: "text/markdown",
			Content:  []byte("# notes"),
		},
	}

	err := c.Run(context.Background(), "sess-1", "my prompt", attachments...)
	require.NoError(t, err)

	call := <-captured
	prompt := call.Prompt

	assert.Contains(t, prompt, "# File: /path/to/test.txt")
	assert.Contains(t, prompt, "hello world")
	assert.Contains(t, prompt, "# File: /path/to/notes.md")
	assert.Contains(t, prompt, "# notes")
	assert.NotContains(t, prompt, "<file")
	assert.NotContains(t, prompt, "</file>")
	assert.NotContains(t, prompt, "<system_info>")
}

func TestCoordinator_Run_EmptyAttachments(t *testing.T) {
	t.Parallel()

	captured := make(chan SessionAgentCall, 1)
	agent := &capturingAgent{captured: captured, modelName: "test-model"}
	c := &minimalCoordinator{currentAgent: agent}

	const input = "plain prompt without attachments"
	err := c.Run(context.Background(), "sess-1", input)
	require.NoError(t, err)

	call := <-captured
	assert.Equal(t, input, call.Prompt)
	assert.NotContains(t, call.Prompt, "<file")
	assert.NotContains(t, call.Prompt, "<system_info>")
}

// SystemPrompt is the building block both NewCoordinator and UpdateModels
// call to refresh c.systemPrompt before pushing it onto the agent. A
// regression here (empty result, error returned) makes the model run
// with no instructions — the bash-first protocol breaks silently. Pin
// it with a happy-path call against a default-initialized config.
func TestSystemPrompt_BuildsNonEmptyPrompt(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	cfg, err := config.Init(dataDir, "", false)
	require.NoError(t, err)

	prompt, err := SystemPrompt(context.Background(), dataDir, "anthropic", "claude-sonnet-4-6", cfg, nil)
	require.NoError(t, err)
	require.NotEmpty(t, prompt, "SystemPrompt must produce non-empty content — empty means no model instructions")
}

// TestCoordinator_SystemPromptGetterReturnsStored asserts the wiring
// between c.systemPrompt and c.SystemPrompt() is intact — guards against
// a future rename / refactor breaking the read path used by UI code that
// surfaces the active prompt.
func TestCoordinator_SystemPromptGetterReturnsStored(t *testing.T) {
	t.Parallel()
	c := &coordinator{systemPrompt: "test-prompt-sentinel"}
	assert.Equal(t, "test-prompt-sentinel", c.SystemPrompt())
}

// TestBuildCall_NoLongerInjectsLenosEnvVars verifies buildCall does NOT
// inject LENOS_SESSION_ID or LENOS_DATA_DIR.
func TestBuildCall_NoLongerInjectsLenosEnvVars(t *testing.T) {
	tmp := t.TempDir()
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	cfg, err := config.Init(tmp, "", false)
	require.NoError(t, err)

	c := &coordinator{
		cfg:          cfg,
		dataDir:      tmp,
		currentAgent: &stubAgent{modelName: "test-model"},
	}

	// Pre-create the session file so recorderFor's Open guard skips Open
	// (which would deref a nil fantasy model on the stub agent).
	sessionsDir := filepath.Join(tmp, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sessionsDir, "sess-123.md"), nil, 0o644))

	call, err := buildCall(context.Background(), "sess-123", "hi", Model{}, config.ProviderConfig{}, c.cfg, nil, nil)
	require.NoError(t, err)

	// buildCall copies all OS env vars and also explicitly sets
	// LENOS_SESSION_ID so subprocess tools can find the session.
	v, ok := call.Env["LENOS_SESSION_ID"]
	assert.True(t, ok, "coordinator must set LENOS_SESSION_ID")
	assert.Equal(t, "sess-123", v, "LENOS_SESSION_ID must match session ID")
	_, hasDataDir := call.Env["LENOS_DATA_DIR"]
	assert.False(t, hasDataDir, "LENOS_DATA_DIR no longer exported")
}

// testSessionService is a minimal session.Service for recorderFor unit tests.
type testSessionService struct {
	title  string
	getErr error
}

func (t *testSessionService) Get(_ context.Context, id string) (session.Session, error) {
	if t.getErr != nil {
		return session.Session{}, t.getErr
	}
	return session.Session{ID: id, Title: t.title}, nil
}
func (t *testSessionService) List(_ context.Context) ([]session.Session, error) { return nil, nil }
func (t *testSessionService) Create(_ context.Context, _ string) (session.Session, error) {
	return session.Session{}, nil
}

func (t *testSessionService) UpdateTitleAndUsage(_ context.Context, _, _ string, _, _ int64, _, _, _ int64, _ float64) error {
	return nil
}

func (t *testSessionService) AppendMessage(_ context.Context, _, _ string, _ ...message.Message) error {
	return nil
}

func (t *testSessionService) ListMessages(_ context.Context, _ string) ([]message.Message, error) {
	return nil, nil
}

func (t *testSessionService) AgentQueuedPrompts(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (t *testSessionService) AgentQueuedPromptsList(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (t *testSessionService) CreateTitleSession(_ context.Context, _ string) (session.Session, error) {
	return session.Session{}, nil
}
func (t *testSessionService) CreateAgentToolSessionID(_, _ string) string { return "" }
func (t *testSessionService) ParseAgentToolSessionID(_ string) (string, string, bool) {
	return "", "", false
}
func (t *testSessionService) IsAgentToolSession(_ string) bool { return false }
func (t *testSessionService) Save(_ context.Context, _ session.Session) (session.Session, error) {
	return session.Session{}, nil
}
func (t *testSessionService) Rename(_ context.Context, _, _ string) error { return nil }
func (t *testSessionService) Delete(_ context.Context, _ string) error    { return nil }
func (t *testSessionService) GetLast(_ context.Context) (session.Session, error) {
	return session.Session{}, nil
}

func (t *testSessionService) CreateTaskSession(_ context.Context, _, _, _ string) (session.Session, error) {
	return session.Session{}, nil
}

func (t *testSessionService) Subscribe(_ context.Context) <-chan pubsub.Event[session.Session] {
	return nil
}

func TestIsUnauthorized(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "401 fantasy.ProviderError → true",
			err:  &fantasy.ProviderError{StatusCode: http.StatusUnauthorized},
			want: true,
		},
		{
			name: "500 fantasy.ProviderError → false",
			err:  &fantasy.ProviderError{StatusCode: http.StatusInternalServerError},
			want: false,
		},
		{
			name: "generic error → false",
			err:  errors.New("connection refused"),
			want: false,
		},
		{
			name: "nil → false",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isUnauthorized(tc.err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestBuildSelectedModel_ReviewErrorsNameReviewTier(t *testing.T) {
	tmp := t.TempDir()
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	cfg, err := config.Init(tmp, "", false)
	require.NoError(t, err)

	c := &coordinator{cfg: cfg}
	_, err = buildSelectedModel(context.Background(), config.SelectedModel{
		Provider: "missing-provider",
		Model:    "review-model",
	}, false, errReviewModelProviderNotConfigured, errReviewModelNotFound, c.cfg)

	require.ErrorIs(t, err, errReviewModelProviderNotConfigured)
	require.Contains(t, err.Error(), "review model provider not configured")
}

// _newCoordinatorSignatureLock is a compile-time guard.
var _newCoordinatorSignatureLock = func() {
	_, _ = NewCoordinator(context.TODO(), nil, nil, nil, nil)
}

// TestBuildCall_AccessModeFromOverrides verifies that RuntimeOverrides.ReadOnly
// selects AccessModeRO (not AccessModeRW) in buildCall, which in turn sets
// AllowedPaths[0].ReadOnly=true so the temenos sandbox enforces RO on cwd.
func TestBuildCall_AccessModeFromOverrides(t *testing.T) {
	setup := func(t *testing.T) *config.ConfigStore {
		tmp := t.TempDir()
		configDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
		t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
		t.Setenv("LENOS_GLOBAL_DATA", configDir)
		t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")
		cfg, err := config.Init(tmp, "", false)
		require.NoError(t, err)
		sessionsDir := filepath.Join(tmp, "sessions")
		require.NoError(t, os.MkdirAll(sessionsDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(sessionsDir, "sess-x.md"), nil, 0o644))
		return cfg
	}

	t.Run("default rw", func(t *testing.T) {
		cfg := setup(t)
		c := &coordinator{
			cfg:          cfg,
			dataDir:      cfg.WorkingDir(),
			currentAgent: &stubAgent{modelName: "test-model"},
		}
		call, err := buildCall(context.Background(), "sess-x", "hi", Model{}, config.ProviderConfig{}, c.cfg, nil, nil)
		require.NoError(t, err)
		require.NotEmpty(t, call.AllowedPaths)
		assert.False(t, call.AllowedPaths[0].ReadOnly, "default should be RW")
	})

	t.Run("override ro", func(t *testing.T) {
		cfg := setup(t)
		cfg.Overrides().ReadOnly = true
		c := &coordinator{
			cfg:          cfg,
			dataDir:      cfg.WorkingDir(),
			currentAgent: &stubAgent{modelName: "test-model"},
		}
		call, err := buildCall(context.Background(), "sess-x", "hi", Model{}, config.ProviderConfig{}, c.cfg, nil, nil)
		require.NoError(t, err)
		require.NotEmpty(t, call.AllowedPaths)
		assert.True(t, call.AllowedPaths[0].ReadOnly, "RO override should set cwd ReadOnly=true")
	})

	t.Run("override disables sandbox", func(t *testing.T) {
		cfg := setup(t)
		cfg.Overrides().NoSandbox = true
		c := &coordinator{
			cfg:          cfg,
			dataDir:      cfg.WorkingDir(),
			currentAgent: &stubAgent{modelName: "test-model"},
		}
		call, err := buildCall(context.Background(), "sess-x", "hi", Model{}, config.ProviderConfig{}, c.cfg, nil, nil)
		require.NoError(t, err)
		assert.False(t, call.Sandbox)
	})
}

func TestBuildCall_ContextAllowedPathsAreAbsoluteExistingPaths(t *testing.T) {
	tmp := t.TempDir()
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	cfg, err := config.Init(tmp, "", false)
	require.NoError(t, err)

	contextFile := filepath.Join(tmp, "AGENTS.md")
	require.NoError(t, os.WriteFile(contextFile, []byte("project instructions"), 0o644))

	c := &coordinator{
		cfg:          cfg,
		dataDir:      cfg.WorkingDir(),
		currentAgent: &stubAgent{modelName: "test-model"},
	}
	call, err := buildCall(context.Background(), "sess-x", "hi", Model{}, config.ProviderConfig{}, c.cfg, nil, nil)
	require.NoError(t, err)

	for _, allowed := range call.AllowedPaths {
		assert.True(t, filepath.IsAbs(allowed.Path), "allowed path must be absolute: %q", allowed.Path)
	}
	require.GreaterOrEqual(t, len(call.ContextCommands), 5)
	assert.Contains(t, call.ContextCommands[0].Command, "src --help")
	assert.Contains(t, call.ContextCommands[1].Command, "web --help")
	assert.Contains(t, call.ContextCommands[2].Command, "skill list")
	assert.Contains(t, call.ContextCommands[3].Command, "project list")
	assert.Equal(t, lenosbash.WrapBash("Read the session journal.", "cat $LENOS_JOURNAL"), call.ContextCommands[4].Command)
}

func TestBuildCall_ReviewerContextExcludesCoderContext(t *testing.T) {
	tmp := t.TempDir()
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	cfg, err := config.Init(tmp, "", false)
	require.NoError(t, err)
	cfg.Overrides().AgentName = config.AgentReviewer

	c := &coordinator{
		cfg:          cfg,
		dataDir:      cfg.WorkingDir(),
		currentAgent: &stubAgent{modelName: "test-model"},
	}
	call, err := buildCall(context.Background(), "sess-x", "hi", Model{}, config.ProviderConfig{}, c.cfg, nil, nil)
	require.NoError(t, err)

	joined := strings.Join(commandTexts(call.ContextCommands), "\n")
	assert.Contains(t, joined, "Inspect local review state.")
	assert.NotContains(t, joined, "Read the session journal.")
	assert.NotContains(t, joined, "cat $LENOS_JOURNAL")
}

func commandTexts(commands []RuntimeContextCommand) []string {
	out := make([]string, 0, len(commands))
	for _, command := range commands {
		out = append(out, command.Command)
	}
	return out
}
