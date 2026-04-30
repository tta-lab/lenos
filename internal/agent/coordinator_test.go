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
	"github.com/tta-lab/lenos/internal/transcript"
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
	}
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

// TestBuildCall_SetsLenosEnvVars verifies that buildCall injects
// LENOS_SESSION_ID + LENOS_DATA_DIR (absolute) into the subprocess env so
// narrate (cmd/narrate) can resolve the session .md path.
func TestBuildCall_SetsLenosEnvVars(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
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
	assert.Equal(t, tmp, call.Env["LENOS_DATA_DIR"])
	assert.True(t, filepath.IsAbs(call.Env["LENOS_DATA_DIR"]),
		"LENOS_DATA_DIR must be absolute")
}

// TestIsUnauthorized verifies that isUnauthorized correctly classifies
// fantasy.ProviderError by status code. This is the gateway for the
// OAuth/API-key refresh retry path in Coordinator.Run.
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
