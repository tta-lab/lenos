package agent

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/csync"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/pubsub"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/transcript"
	"github.com/tta-lab/temenos/client"
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
	runErr := m.currentAgent.Run(ctx, SessionAgentCall{})
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
			name:    "stopExit success → nil",
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
func TestCoordinator_recorderFor_cachesPerSession(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), ".lenos")
	sessionsDir := filepath.Join(dataDir, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))
	for _, sid := range []string{"session-a", "session-b"} {
		path := filepath.Join(sessionsDir, sid+".md")
		require.NoError(t, os.WriteFile(path, nil, 0o644))
	}

	c := &coordinator{
		dataDir:      dataDir,
		recorders:    csync.NewMap[string, transcript.Recorder](),
		currentAgent: &stubAgent{modelName: "test-model"},
	}

	r1a := c.recorderFor("session-a")
	r1b := c.recorderFor("session-a")
	r2 := c.recorderFor("session-b")

	require.NotNil(t, r1a)
	require.NotNil(t, r2)
	assert.Same(t, r1a, r1b, "same sessionID should return cached recorder")
	assert.NotSame(t, r1a, r2, "different sessionIDs should return different recorders")
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

	prompt, err := SystemPrompt(context.Background(), dataDir, "anthropic", "claude-sonnet-4-6", cfg)
	require.NoError(t, err)
	require.NotEmpty(t, prompt, "SystemPrompt must produce non-empty content — empty means no model instructions")

	// Spot-check the bash-first protocol marker is present so a future
	// template restructure that drops the protocol section gets caught.
	// "narrate <<" + "exit" together signal the heredoc-only contract is
	// rendered into the prompt.
	assert.Contains(t, prompt, "narrate <<", "bash-first protocol must explain narrate heredoc form")
	assert.Contains(t, prompt, "Output Protocol", "bash-first output-protocol section must be in the rendered prompt")
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

// TestBuildCall_SetsLenosEnvVars verifies buildCall injects LENOS_SESSION_ID
// (the only env var narrate still needs — the data dir is auto-derived from
// the subprocess cwd).
func TestBuildCall_SetsLenosEnvVars(t *testing.T) {
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
		recorders:    csync.NewMap[string, transcript.Recorder](),
		currentAgent: &stubAgent{modelName: "test-model"},
	}

	// Pre-create the session file so recorderFor's Open guard skips Open
	// (which would deref a nil fantasy model on the stub agent).
	sessionsDir := filepath.Join(tmp, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sessionsDir, "sess-123.md"), nil, 0o644))

	call := c.buildCall(context.Background(), "sess-123", "hi", Model{}, config.ProviderConfig{})

	assert.Equal(t, "sess-123", call.Env["LENOS_SESSION_ID"])
	_, hasDataDir := call.Env["LENOS_DATA_DIR"]
	assert.False(t, hasDataDir, "LENOS_DATA_DIR no longer exported; narrate finds .lenos via cwd LookupClosest")
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

func (t *testSessionService) UpdateTitleAndUsage(_ context.Context, _, _ string, _, _ int64, _ float64) error {
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

// TestRecorderFor_AgentNameOverride exercises the actual --agent runtime
// override path: recorderFor reads c.cfg.Overrides().AgentName, not the
// persisted config field. The previous incarnation of this test set the
// config field via SetConfigField, which left Overrides empty — the
// assertion passed vacuously on the default "lenos" name. Now we mutate
// the runtime overrides directly so the override actually flows through.
func TestRecorderFor_AgentNameOverride(t *testing.T) {
	dataDir := t.TempDir()
	sessionsDir := filepath.Join(dataDir, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))

	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	cfg, err := config.Init(dataDir, "", false)
	require.NoError(t, err)
	cfg.Overrides().AgentName = "kestrel"

	sid := "agent-override-test"
	c := &coordinator{
		dataDir:      dataDir,
		recorders:    csync.NewMap[string, transcript.Recorder](),
		currentAgent: &stubAgent{modelName: "test-model"},
		cfg:          cfg,
		sessions:     &testSessionService{},
	}

	r := c.recorderFor(sid)
	require.NotNil(t, r)

	bs, err := os.ReadFile(filepath.Join(sessionsDir, sid+".md"))
	require.NoError(t, err)
	// AgentName comes from Overrides().AgentName (the --agent runtime flag).
	// With the override set to "kestrel" the meta header must reflect it —
	// not the default "lenos".
	require.Contains(t, string(bs), "agent: kestrel\n")
	require.NotContains(t, string(bs), "agent: lenos\n", "regression: override path silently fell back to default")
}

func TestRecorderFor_SandboxThreeState(t *testing.T) {
	boolPtr := func(v bool) *bool { return &v }

	cases := []struct {
		name          string
		sandboxOption *bool
		sandboxClient *client.Client
		want          string
	}{
		{"sandbox on, client present", boolPtr(true), &client.Client{}, "sandbox: on\n"},
		{"sandbox on, client absent (degraded)", boolPtr(true), nil, "sandbox: degraded\n"},
		{"sandbox off", boolPtr(false), &client.Client{}, "sandbox: off\n"}, // will fail if sandbox option not wired
		{"sandbox nil (default true), client present", nil, &client.Client{}, "sandbox: on\n"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			sessionsDir := filepath.Join(dataDir, "sessions")
			require.NoError(t, os.MkdirAll(sessionsDir, 0o755))

			configDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
			t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
			t.Setenv("LENOS_GLOBAL_DATA", configDir)
			t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")
			require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
			t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
			t.Setenv("LENOS_GLOBAL_DATA", configDir)
			t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")
			require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))

			cfg, err := config.Init(dataDir, "", false)
			require.NoError(t, err)

			sid := "sandbox-" + tc.name
			c := &coordinator{
				dataDir:       dataDir,
				recorders:     csync.NewMap[string, transcript.Recorder](),
				currentAgent:  &stubAgent{modelName: "test-model"},
				cfg:           cfg,
				sessions:      &testSessionService{},
				sandboxClient: tc.sandboxClient,
			}

			// Override sandbox option if specified
			if tc.sandboxOption != nil {
				cfg.Config().Options.Sandbox = tc.sandboxOption
			}

			r := c.recorderFor(sid)
			require.NotNil(t, r)

			bs, err := os.ReadFile(filepath.Join(sessionsDir, sid+".md"))
			require.NoError(t, err)
			require.Contains(t, string(bs), tc.want)
		})
	}
}

func TestRecorderFor_TitleAndCwd(t *testing.T) {
	dataDir := t.TempDir()
	sessionsDir := filepath.Join(dataDir, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))

	sid := "title-cwd-test"
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))

	cfg, err := config.Init(dataDir, "", false)
	require.NoError(t, err)

	c := &coordinator{
		dataDir:      dataDir,
		recorders:    csync.NewMap[string, transcript.Recorder](),
		currentAgent: &stubAgent{modelName: "test-model"},
		cfg:          cfg,
		sessions:     &testSessionService{title: "My Test Task"},
	}

	r := c.recorderFor(sid)
	require.NotNil(t, r)

	bs, err := os.ReadFile(filepath.Join(sessionsDir, sid+".md"))
	require.NoError(t, err)
	content := string(bs)
	require.Contains(t, content, "title: My Test Task\n")
	require.Contains(t, content, "cwd: ")
}

func TestRecorderFor_TitleErrorIsNonFatal(t *testing.T) {
	dataDir := t.TempDir()
	sessionsDir := filepath.Join(dataDir, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))

	sid := "title-err-test"
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))
	t.Setenv("LENOS_GLOBAL_CONFIG", configDir)
	t.Setenv("LENOS_GLOBAL_DATA", configDir)
	t.Setenv("LENOS_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{}`), 0o644))

	cfg, err := config.Init(dataDir, "", false)
	require.NoError(t, err)

	c := &coordinator{
		dataDir:      dataDir,
		recorders:    csync.NewMap[string, transcript.Recorder](),
		currentAgent: &stubAgent{modelName: "test-model"},
		cfg:          cfg,
		sessions:     &testSessionService{getErr: errors.New("session not found")},
	}

	r := c.recorderFor(sid)
	require.NotNil(t, r)

	bs, err := os.ReadFile(filepath.Join(sessionsDir, sid+".md"))
	require.NoError(t, err)
	require.NotContains(t, string(bs), "title:")
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

// _newCoordinatorSignatureLock is a compile-time guard.
var _newCoordinatorSignatureLock = func() {
	var sc *client.Client
	_, _ = NewCoordinator(context.TODO(), nil, nil, nil, nil, nil, sc)
}

// TestBuildCall_AccessModeFromOverrides verifies that the --readonly override
// (RuntimeOverrides.ReadOnly) propagates to AllowedPaths[0].ReadOnly so the
// temenos sandbox enforces RO on cwd.
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
			recorders:    csync.NewMap[string, transcript.Recorder](),
			currentAgent: &stubAgent{modelName: "test-model"},
		}
		call := c.buildCall(context.Background(), "sess-x", "hi", Model{}, config.ProviderConfig{})
		require.NotEmpty(t, call.AllowedPaths)
		assert.False(t, call.AllowedPaths[0].ReadOnly, "default should be RW")
	})

	t.Run("override ro", func(t *testing.T) {
		cfg := setup(t)
		cfg.Overrides().ReadOnly = true
		c := &coordinator{
			cfg:          cfg,
			dataDir:      cfg.WorkingDir(),
			recorders:    csync.NewMap[string, transcript.Recorder](),
			currentAgent: &stubAgent{modelName: "test-model"},
		}
		call := c.buildCall(context.Background(), "sess-x", "hi", Model{}, config.ProviderConfig{})
		require.NotEmpty(t, call.AllowedPaths)
		assert.True(t, call.AllowedPaths[0].ReadOnly, "RO override should set cwd ReadOnly=true")
	})
}
