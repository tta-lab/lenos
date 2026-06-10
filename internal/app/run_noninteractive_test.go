package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/agent"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/pubsub"
)

type runTestCoordinator struct {
	err error
}

func (c *runTestCoordinator) Run(ctx context.Context, _ string, _ string, _ ...message.Attachment) error {
	summary := agent.RunUsageSummaryFromContext(ctx)
	if summary != nil {
		summary.AddUsage(agent.Model{
			ModelCfg: config.SelectedModel{Provider: "deepseek", Model: "deepseek-v4-flash"},
		}, fantasy.Usage{
			InputTokens:     10,
			OutputTokens:    5,
			CacheReadTokens: 20,
		}, 0.00123)
	}
	return c.err
}
func (c *runTestCoordinator) RunRuntime(_ context.Context, _ string, _ string) error { return c.err }

func (c *runTestCoordinator) Cancel(string)                                           {}
func (c *runTestCoordinator) CancelAll()                                              {}
func (c *runTestCoordinator) IsSessionBusy(string) bool                               { return false }
func (c *runTestCoordinator) IsBusy() bool                                            { return false }
func (c *runTestCoordinator) QueuedPrompts(string) int                                { return 0 }
func (c *runTestCoordinator) QueuedPromptsList(string) []string                       { return nil }
func (c *runTestCoordinator) ClearQueue(string)                                       {}
func (c *runTestCoordinator) ActiveBackgroundJobs(string) []agent.BackgroundJob       { return nil }
func (c *runTestCoordinator) KillBackgroundJob(context.Context, string, string) error { return nil }
func (c *runTestCoordinator) StopBackgroundJobs(string)                               {}
func (c *runTestCoordinator) Summarize(context.Context, string) error                 { return nil }
func (c *runTestCoordinator) Model() agent.Model                                      { return agent.Model{} }
func (c *runTestCoordinator) UpdateModels(context.Context) error                      { return nil }
func (c *runTestCoordinator) CompactSession(context.Context, string) error            { return c.err }

func (c *runTestCoordinator) SystemPrompt() string { return "" }

type noopMessageService struct{}

func (noopMessageService) Subscribe(context.Context) <-chan pubsub.Event[message.Message] {
	return make(chan pubsub.Event[message.Message])
}

func (noopMessageService) Create(context.Context, string, message.CreateMessageParams) (message.Message, error) {
	return message.Message{}, nil
}
func (noopMessageService) Update(context.Context, message.Message) error { return nil }
func (noopMessageService) Get(context.Context, string) (message.Message, error) {
	return message.Message{}, nil
}

func (noopMessageService) List(context.Context, string) ([]message.Message, error) {
	return nil, nil
}

func (noopMessageService) ListUserMessages(context.Context, string) ([]message.Message, error) {
	return nil, nil
}

func (noopMessageService) ListAllUserMessages(context.Context) ([]message.Message, error) {
	return nil, nil
}
func (noopMessageService) Delete(context.Context, string) error                { return nil }
func (noopMessageService) DeleteSessionMessages(context.Context, string) error { return nil }

func newRunNonInteractiveTestApp(t *testing.T, coordinator agent.Coordinator) *App {
	t.Helper()

	configDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("XDG_DATA_HOME", dataDir)

	store, err := config.Load(t.TempDir(), dataDir, false)
	require.NoError(t, err)

	return &App{
		Sessions:         &mockSessionService{},
		Messages:         noopMessageService{},
		AgentCoordinator: coordinator,
		config:           store,
	}
}

func TestRunNonInteractive_WritesUsageSummaryOnAgentError(t *testing.T) {
	app := newRunNonInteractiveTestApp(t, &runTestCoordinator{err: errors.New("boom")})
	path := filepath.Join(t.TempDir(), "usage.json")

	err := app.RunNonInteractive(t.Context(), &bytes.Buffer{}, "go", true, "", false, path, "")

	require.ErrorContains(t, err, "agent processing failed")
	data := requireReadFile(t, path)

	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	require.Equal(t, "run_summary", got["event"])
	require.Equal(t, "new-session-id", got["session_id"])
	require.Equal(t, "deepseek", got["provider_id"])
	require.Equal(t, "deepseek-v4-flash", got["model_id"])
	require.Equal(t, float64(30), got["input_tokens"])
	require.Equal(t, float64(35), got["total_tokens"])
	require.Equal(t, 0.00123, got["cost_usd"])
}

func TestRunNonInteractive_UsageSummaryPathFailureReturnsError(t *testing.T) {
	app := newRunNonInteractiveTestApp(t, &runTestCoordinator{})
	path := filepath.Join(t.TempDir(), "missing", "usage.json")

	err := app.RunNonInteractive(t.Context(), &bytes.Buffer{}, "go", true, "", false, path, "")

	require.ErrorContains(t, err, "write usage summary")
}

func requireReadFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}
