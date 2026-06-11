package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/agent/lenosbash"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/pubsub"
)

// mockMessageService is a minimal in-memory message.Service for unit tests.
// Reused across loop_test.go, agent_run_test.go, and any other test that
// needs a Service without a real DB.
type mockMessageService struct {
	mu       sync.Mutex
	messages map[string]message.Message
	order    []string // insertion order for deterministic iteration
	updates  []message.Message
	idSeq    int
}

func newMockMessageService() *mockMessageService {
	return &mockMessageService{messages: make(map[string]message.Message)}
}

func (m *mockMessageService) Subscribe(_ context.Context) <-chan pubsub.Event[message.Message] {
	return nil
}

func (m *mockMessageService) Create(_ context.Context, _ string, params message.CreateMessageParams) (message.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.idSeq++
	id := string(params.Role) + "-" + string(rune('a'+m.idSeq))
	msg := message.Message{
		ID:       id,
		Role:     params.Role,
		Parts:    params.Parts,
		Model:    params.Model,
		Provider: params.Provider,
	}
	m.messages[id] = msg
	m.order = append(m.order, id)
	return msg, nil
}

func (m *mockMessageService) Update(_ context.Context, msg message.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.messages[msg.ID]; ok {
		msg.CreatedAt = existing.CreatedAt
		msg.UpdatedAt = existing.UpdatedAt
	}
	m.messages[msg.ID] = msg
	clone := msg.Clone()
	m.updates = append(m.updates, clone)
	return nil
}

func (m *mockMessageService) Get(_ context.Context, id string) (message.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if msg, ok := m.messages[id]; ok {
		return msg, nil
	}
	return message.Message{}, nil
}

func (m *mockMessageService) List(_ context.Context, _ string) ([]message.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]message.Message, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.messages[id])
	}
	return out, nil
}

func (m *mockMessageService) ListUserMessages(_ context.Context, _ string) ([]message.Message, error) {
	return nil, nil
}

func (m *mockMessageService) ListAllUserMessages(context.Context) ([]message.Message, error) {
	return nil, nil
}

func (m *mockMessageService) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.messages, id)
	for i, existingID := range m.order {
		if existingID == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	return nil
}
func (m *mockMessageService) DeleteSessionMessages(_ context.Context, _ string) error { return nil }

// mockLanguageModel implements fantasy.LanguageModel for tests that just need
// a placeholder. Tests that drive the loop should use scriptedModel from
// loop_test.go.
type mockLanguageModel struct{}

func (m *mockLanguageModel) Model() string    { return "test-model" }
func (m *mockLanguageModel) Provider() string { return "test-provider" }
func (m *mockLanguageModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	panic("not implemented")
}

func (m *mockLanguageModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	panic("not implemented")
}

func (m *mockLanguageModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	panic("not implemented")
}

func (m *mockLanguageModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	panic("not implemented")
}

// mockProvider implements fantasy.Provider for test construction.
type mockProvider struct{}

func (p *mockProvider) Name() string { return "test-provider" }

func (p *mockProvider) LanguageModel(_ context.Context, _ string) (fantasy.LanguageModel, error) {
	return &mockLanguageModel{}, nil
}

var _ fantasy.Provider = (*mockProvider)(nil)

func TestBuildHistory_DoesNotIncludePrompt(t *testing.T) {
	t.Parallel()
	existing := []message.Message{
		{ID: "1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hello"}}},
		{ID: "2", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "hi there"}}},
	}
	history := buildHistory(existing)
	require.NotEmpty(t, history)
	last := history[len(history)-1]
	// runLoop appends the prompt internally; buildHistory must not duplicate it.
	assert.NotEqual(t, fantasy.MessageRoleUser, last.Role, "buildHistory must not append the prompt as a user message")
	assert.Equal(t, fantasy.MessageRoleAssistant, last.Role, "last element should be the assistant reply")
}

func TestRun_PersistsRuntimeContextCommandsBeforeUserPrompt(t *testing.T) {
	t.Parallel()
	env := testEnv(t)

	contextFile := filepath.Join(env.workingDir, "context.md")
	require.NoError(t, os.WriteFile(contextFile, []byte("project instructions"), 0o644))

	inner := &scriptedModel{emits: []string{"exit"}}
	model := &streamCapturingModel{inner: inner}
	primary := Model{
		Model:      model,
		CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 1024},
		ModelCfg:   config.SelectedModel{Provider: "test-provider", Model: "test-model"},
	}
	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:   primary,
		SmallModel:   primary,
		PrimaryModel: primary,
		SystemPrompt: "system prompt",
		Sessions:     env.sessions,
		Messages:     env.messages,
	}).(*sessionAgent)
	sess, err := env.sessions.Create(t.Context(), "runtime context")
	require.NoError(t, err)

	err = agent.Run(t.Context(), SessionAgentCall{
		SessionID: sess.ID,
		Prompt:    "user prompt",
		AllowedPaths: []AllowedPath{
			{Path: env.workingDir},
		},
		ContextCommands: []RuntimeContextCommand{{
			Command: lenosbash.WrapBash("Read key instructions.", "cat "+shellQuote(contextFile)),
		}, {
			Command: "\nReady.\n\nLets rock and roll.\n",
		}},
	})
	require.NoError(t, err)

	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(msgs), 4)
	require.Equal(t, message.Assistant, msgs[0].Role)
	require.Equal(t, lenosbash.WrapBash("Read key instructions.", "cat "+shellQuote(contextFile)), msgs[0].Content().Text)
	require.Equal(t, message.FinishReasonToolUse, msgs[0].FinishReason())
	require.Equal(t, message.Result, msgs[1].Role)
	require.Equal(t, "cat "+shellQuote(contextFile)+"\n", msgs[1].CommandContent().Command)
	require.Equal(t, "project instructions", msgs[1].CommandContent().Output)
	require.Equal(t, message.Assistant, msgs[2].Role)
	require.Equal(t, "\nReady.\n\nLets rock and roll.\n", msgs[2].Content().Text)
	require.Nil(t, msgs[2].FinishPart())
	require.Equal(t, "test-model", msgs[2].Model)
	require.Equal(t, "test-provider", msgs[2].Provider)
	require.Equal(t, message.User, msgs[3].Role)
	require.Equal(t, "user prompt", msgs[3].Content().Text)

	require.Len(t, model.Captured(), 1)
	prompt := model.Captured()[0]
	require.Len(t, prompt, 5)
	require.Equal(t, fantasy.MessageRoleSystem, prompt[0].Role)
	require.Equal(t, fantasy.MessageRoleAssistant, prompt[1].Role)
	require.Equal(t, fantasy.MessageRoleUser, prompt[2].Role)
	require.Contains(t, fantasyMessageText(prompt[2]), "project instructions")
	require.Equal(t, fantasy.MessageRoleAssistant, prompt[3].Role)
	require.Equal(t, "Ready.\n\nLets rock and roll.", fantasyMessageText(prompt[3]))
	require.Equal(t, "user prompt", fantasyMessageText(prompt[4]))
}

func TestPersistRuntimeContextCommands_ExecutesCleanBashFromMixedProseAndBash(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	primary := Model{
		Model:      &mockLanguageModel{},
		CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 1024},
		ModelCfg:   config.SelectedModel{Provider: "test-provider", Model: "test-model"},
	}
	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:   primary,
		SmallModel:   primary,
		PrimaryModel: primary,
		SystemPrompt: "system prompt",
		Sessions:     env.sessions,
		Messages:     env.messages,
	}).(*sessionAgent)
	sess, err := env.sessions.Create(t.Context(), "runtime context")
	require.NoError(t, err)

	raw := lenosbash.WrapBash("List registered projects and available skills.", "ttal project list\nskill list")
	runner := &fakeRunner{results: []ExecResult{{
		Stdout:   []byte("project-a\nskill-a\n"),
		ExitCode: 0,
	}}}
	err = agent.persistRuntimeContextCommands(t.Context(), SessionAgentCall{
		SessionID: sess.ID,
		ContextCommands: []RuntimeContextCommand{{
			Command: raw,
		}},
	}, runner)
	require.NoError(t, err)

	require.Equal(t, []string{"ttal project list\nskill list\n"}, runner.bash)

	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, message.Assistant, msgs[0].Role)
	require.Equal(t, raw, msgs[0].Content().Text)
	require.Equal(t, message.FinishReasonToolUse, msgs[0].FinishReason())
	require.Equal(t, message.Result, msgs[1].Role)
	require.Equal(t, "ttal project list\nskill list\n", msgs[1].CommandContent().Command)
	require.Equal(t, "project-a\nskill-a\n", msgs[1].CommandContent().Output)
}

func TestRun_GeneratesTaskTitleWhenRuntimeContextInjected(t *testing.T) {
	env := testEnv(t)

	contextFile := filepath.Join(env.workingDir, "context.md")
	require.NoError(t, os.WriteFile(contextFile, []byte("project instructions"), 0o644))

	primary := Model{
		Model:      &scriptedModel{emits: []string{"exit"}},
		CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 1024},
		ModelCfg:   config.SelectedModel{Provider: "test-provider", Model: "test-model"},
	}
	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:   primary,
		SmallModel:   primary,
		PrimaryModel: primary,
		SystemPrompt: "system prompt",
		Sessions:     env.sessions,
		Messages:     env.messages,
	}).(*sessionAgent)
	agent.taskExporter = func(context.Context, string) ([]byte, error) {
		return []byte(`[{"description":"Fix synthetic context title","status":"pending"}]`), nil
	}
	sess, err := env.sessions.Create(t.Context(), "runtime context")
	require.NoError(t, err)

	err = agent.Run(t.Context(), SessionAgentCall{
		SessionID: sess.ID,
		Prompt:    "user prompt",
		TaskID:    "25620b89",
		AllowedPaths: []AllowedPath{
			{Path: env.workingDir},
		},
		ContextCommands: []RuntimeContextCommand{{
			Command: "# read project instructions\ncat " + shellQuote(contextFile),
		}},
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		updated, err := env.sessions.Get(t.Context(), sess.ID)
		require.NoError(t, err)
		return updated.Title == "Fix synthetic context title"
	}, time.Second, 10*time.Millisecond)
}

func TestRun_RefreshesTaskTitleOnExistingSession(t *testing.T) {
	env := testEnv(t)

	primary := Model{
		Model:      &scriptedModel{emits: []string{"exit"}},
		CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 1024},
		ModelCfg:   config.SelectedModel{Provider: "test-provider", Model: "test-model"},
	}
	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:   primary,
		SmallModel:   primary,
		PrimaryModel: primary,
		SystemPrompt: "system prompt",
		Sessions:     env.sessions,
		Messages:     env.messages,
	}).(*sessionAgent)
	agent.taskExporter = func(context.Context, string) ([]byte, error) {
		return []byte(`[{"description":"Updated task title","status":"pending"}]`), nil
	}
	sess, err := env.sessions.Create(t.Context(), "Old title")
	require.NoError(t, err)
	_, err = env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "previous prompt"}},
	})
	require.NoError(t, err)

	err = agent.Run(t.Context(), SessionAgentCall{
		SessionID: sess.ID,
		Prompt:    "user prompt",
		TaskID:    "25620b89",
		AllowedPaths: []AllowedPath{
			{Path: env.workingDir},
		},
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		updated, err := env.sessions.Get(t.Context(), sess.ID)
		require.NoError(t, err)
		return updated.Title == "Updated task title"
	}, time.Second, 10*time.Millisecond)
}

func fantasyMessageText(msg fantasy.Message) string {
	var sb strings.Builder
	for _, part := range msg.Content {
		switch p := part.(type) {
		case fantasy.TextPart:
			sb.WriteString(p.Text)
		case *fantasy.TextPart:
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

func TestSaveSessionUsage_UpdatesTokenCounts(t *testing.T) {
	t.Parallel()
	env := testEnv(t)

	sess, err := env.sessions.Create(t.Context(), "test session")
	require.NoError(t, err)
	require.Equal(t, int64(0), sess.PromptTokens, "sanity: tokens start at zero")

	lm := &mockLanguageModel{}
	agent := testSessionAgent(env, lm, lm, "sys").(*sessionAgent)

	agent.largeModel.Set(Model{
		Model: lm,
		CatwalkCfg: catwalk.Model{
			ContextWindow:    200000,
			DefaultMaxTokens: 8096,
			CostPer1MIn:      3.0,
			CostPer1MOut:     15.0,
		},
	})

	agent.primaryModel.Set(Model{
		Model: lm,
		CatwalkCfg: catwalk.Model{
			ContextWindow:    200000,
			DefaultMaxTokens: 8096,
			CostPer1MIn:      3.0,
			CostPer1MOut:     15.0,
		},
	})

	usage := fantasy.Usage{
		InputTokens:         1000,
		OutputTokens:        500,
		CacheCreationTokens: 100,
		CacheReadTokens:     200,
	}

	updated, ok := agent.saveSessionUsage(t.Context(), sess.ID, usage, nil, "save failed")
	require.True(t, ok, "saveSessionUsage should succeed")
	assert.Equal(t, int64(1200), updated.PromptTokens, "PromptTokens should reflect current prompt input including cache reads")
	assert.Equal(t, int64(500), updated.CompletionTokens, "CompletionTokens should reflect OutputTokens")
	assert.Equal(t, int64(1000), updated.CacheMissTokens)
	assert.Equal(t, int64(100), updated.CacheCreationTokens)
	assert.Equal(t, int64(200), updated.CacheReadTokens)
	assert.Greater(t, updated.Cost, 0.0, "Cost should be non-zero")

	updated, ok = agent.saveSessionUsage(t.Context(), sess.ID, fantasy.Usage{
		InputTokens:     10,
		OutputTokens:    5,
		CacheReadTokens: 90,
	}, nil, "save failed")
	require.True(t, ok, "saveSessionUsage should succeed")
	assert.Equal(t, int64(100), updated.PromptTokens, "PromptTokens should remain current-context usage")
	assert.Equal(t, int64(5), updated.CompletionTokens, "CompletionTokens should remain current-context usage")
	assert.Equal(t, int64(1010), updated.CacheMissTokens)
	assert.Equal(t, int64(100), updated.CacheCreationTokens)
	assert.Equal(t, int64(290), updated.CacheReadTokens)

	persisted, err := env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(100), persisted.PromptTokens)
	assert.Equal(t, int64(5), persisted.CompletionTokens)
	assert.Equal(t, int64(1010), persisted.CacheMissTokens)
	assert.Equal(t, int64(100), persisted.CacheCreationTokens)
	assert.Equal(t, int64(290), persisted.CacheReadTokens)
}

func TestSaveSessionUsage_PricesUncachedInputAndOutput(t *testing.T) {
	t.Parallel()
	env := testEnv(t)

	sess, err := env.sessions.Create(t.Context(), "uncached cost")
	require.NoError(t, err)

	lm := &mockLanguageModel{}
	agent := testSessionAgent(env, lm, lm, "sys").(*sessionAgent)
	model := Model{
		Model: lm,
		CatwalkCfg: catwalk.Model{
			CostPer1MIn:  2,
			CostPer1MOut: 10,
		},
	}
	agent.largeModel.Set(model)
	agent.primaryModel.Set(model)

	updated, ok := agent.saveSessionUsage(t.Context(), sess.ID, fantasy.Usage{
		InputTokens:  1000,
		OutputTokens: 100,
	}, nil, "save failed")
	require.True(t, ok)

	require.InDelta(t, 0.003, updated.Cost, 0.0000001)
}

func TestSaveSessionUsage_PricesCacheReadWithOutputCachedRate(t *testing.T) {
	t.Parallel()
	env := testEnv(t)

	sess, err := env.sessions.Create(t.Context(), "cache read cost")
	require.NoError(t, err)

	lm := &mockLanguageModel{}
	agent := testSessionAgent(env, lm, lm, "sys").(*sessionAgent)
	model := Model{
		Model: lm,
		CatwalkCfg: catwalk.Model{
			CostPer1MIn:        2,
			CostPer1MOut:       10,
			CostPer1MInCached:  0.5,
			CostPer1MOutCached: 0.25,
		},
	}
	agent.largeModel.Set(model)
	agent.primaryModel.Set(model)

	updated, ok := agent.saveSessionUsage(t.Context(), sess.ID, fantasy.Usage{
		InputTokens:     1000,
		OutputTokens:    100,
		CacheReadTokens: 2000,
	}, nil, "save failed")
	require.True(t, ok)

	require.InDelta(t, 0.0035, updated.Cost, 0.0000001)
}

func TestSaveSessionUsage_PricesCacheCreationWithInputCachedRate(t *testing.T) {
	t.Parallel()
	env := testEnv(t)

	sess, err := env.sessions.Create(t.Context(), "cache creation cost")
	require.NoError(t, err)

	lm := &mockLanguageModel{}
	agent := testSessionAgent(env, lm, lm, "sys").(*sessionAgent)
	model := Model{
		Model: lm,
		CatwalkCfg: catwalk.Model{
			CostPer1MInCached:  0.5,
			CostPer1MOutCached: 99,
		},
	}
	agent.largeModel.Set(model)
	agent.primaryModel.Set(model)

	updated, ok := agent.saveSessionUsage(t.Context(), sess.ID, fantasy.Usage{
		CacheCreationTokens: 2000,
	}, nil, "save failed")
	require.True(t, ok)

	require.InDelta(t, 0.001, updated.Cost, 0.0000001)
}

func TestRun_BusySession_QueuesPrompt(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "queue test")
	require.NoError(t, err)

	agent := testSessionAgent(env, nil, nil, "sys").(*sessionAgent)

	// Manually register the session as busy (simulates what Run does when a
	// goroutine starts processing a prompt). This avoids timing races with
	// goroutine scheduling.
	ctx, cancel := context.WithCancel(t.Context())
	agent.activeRequests.Set(sess.ID, cancel)

	// Verify the session is busy.
	require.True(t, agent.IsSessionBusy(sess.ID), "session should be busy")

	// Second call should queue silently and return nil.
	err = agent.Run(ctx, SessionAgentCall{
		SessionID: sess.ID,
		Prompt:    "second",
	})
	require.NoError(t, err, "queueing a prompt on a busy session should return nil")

	// QueuedPrompts should reflect the queued call.
	require.Equal(t, 1, agent.QueuedPrompts(sess.ID), "one prompt should be queued")

	cancel()
	agent.activeRequests.Del(sess.ID)
}

// blockingModel stalls on Run/Stream until the unblock channel closes.
type blockingModel struct {
	unblock chan struct{}
}

func (m *blockingModel) Model() string    { return "blocking-model" }
func (m *blockingModel) Provider() string { return "test" }
func (m *blockingModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	<-m.unblock
	return &fantasy.Response{}, nil
}

func (m *blockingModel) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	select {
	case <-m.unblock:
	case <-ctx.Done():
	}
	return nil, ctx.Err()
}

func (m *blockingModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	panic("not used")
}

func (m *blockingModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	panic("not used")
}

func TestCombineQueuedCalls_SingleCall(t *testing.T) {
	t.Parallel()
	calls := []SessionAgentCall{{
		SessionID: "s1",
		Prompt:    "hello",
	}}
	out := combineQueuedCalls(calls)
	require.Equal(t, "hello", out.Prompt)
	require.Equal(t, "s1", out.SessionID)
}

// recRunner records every Run call's payload for assertions.
type recRunner struct {
	mu      sync.Mutex
	calls   [][]byte
	started chan struct{}
}

func newRecRunner(expected int) *recRunner {
	return &recRunner{started: make(chan struct{}, expected)}
}

func (r *recRunner) Run(_ context.Context, p []byte) error {
	r.started <- struct{}{}
	r.mu.Lock()
	r.calls = append(r.calls, append([]byte(nil), p...))
	r.mu.Unlock()
	return nil
}

func (r *recRunner) Wait(expected int) {
	for i := 0; i < expected; i++ {
		<-r.started
	}
}

// errRunner always returns an error.
type errRunner struct{}

func (errRunner) Run(context.Context, []byte) error { return errors.New("boom") }

// blockRunner blocks until the context expires.
type blockRunner struct{ gate chan struct{} }

func (b *blockRunner) Run(ctx context.Context, _ []byte) error {
	select {
	case <-b.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestRun_HookRunnerFiresPerStep(t *testing.T) {
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "hook test")
	require.NoError(t, err)

	rec := newRecRunner(2) // 2 steps: echo hi + exit
	bm := &scriptedModel{emits: []string{lenosbash.BashBlock("echo hi"), "exit"}}
	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:   Model{Model: bm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		SmallModel:   Model{Model: bm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		PrimaryModel: Model{Model: bm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		SystemPrompt: "sys",
		Sessions:     env.sessions,
		Messages:     env.messages,
		HookRunner:   rec,
	}).(*sessionAgent)

	err = agent.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "go"})
	require.NoError(t, err)

	// Wait for hook goroutines to complete (deterministic: blocks until N Run calls)
	rec.Wait(2)

	rec.mu.Lock()
	require.Len(t, rec.calls, 2, "hook should fire 2 times (exec + exit)")
	// Verify envelope structure
	for i, payload := range rec.calls {
		var ev map[string]any
		require.NoError(t, json.Unmarshal(payload, &ev))
		assert.Equal(t, float64(1), ev["version"])
		assert.Equal(t, "post_step", ev["event"])
		assert.Equal(t, float64(i), ev["step_index"])
		assert.Equal(t, sess.ID, ev["session_id"])
	}
	rec.mu.Unlock()
}

func TestRun_HookRunnerFailingDoesNotAbortLoop(t *testing.T) {
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "hook fail test")
	require.NoError(t, err)

	bm := &scriptedModel{emits: []string{lenosbash.BashBlock("echo one"), lenosbash.BashBlock("echo two"), "exit"}}
	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:   Model{Model: bm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		SmallModel:   Model{Model: bm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		PrimaryModel: Model{Model: bm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		SystemPrompt: "sys",
		Sessions:     env.sessions,
		Messages:     env.messages,
		HookRunner:   errRunner{},
	}).(*sessionAgent)

	err = agent.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "go"})
	require.NoError(t, err, "loop should not abort on hook failure")
}

func TestRun_HookRunnerNoopGating(t *testing.T) {
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "hook noop test")
	require.NoError(t, err)

	bm := &scriptedModel{emits: []string{"exit"}}
	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:   Model{Model: bm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		SmallModel:   Model{Model: bm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		PrimaryModel: Model{Model: bm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		SystemPrompt: "sys",
		Sessions:     env.sessions,
		Messages:     env.messages,
		// HookRunner is nil — no goroutines should be spawned
	}).(*sessionAgent)

	before := runtime.NumGoroutine()
	err = agent.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "go"})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond) // settle goroutines
	after := runtime.NumGoroutine()

	// Allow small variance from scheduler goroutines
	diff := after - before
	if diff > 3 {
		t.Fatalf("possible goroutine leak: %d → %d (diff %d)", before, after, diff)
	}
}

func TestRun_HookRunnerTimeout(t *testing.T) {
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "hook timeout test")
	require.NoError(t, err)

	blk := &blockRunner{gate: make(chan struct{})}
	bm := &scriptedModel{emits: []string{"exit"}}
	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:   Model{Model: bm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		SmallModel:   Model{Model: bm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		PrimaryModel: Model{Model: bm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		SystemPrompt: "sys",
		Sessions:     env.sessions,
		Messages:     env.messages,
		HookRunner:   blk,
	}).(*sessionAgent)

	// Shrink timeout so test runs in 50ms instead of 5s
	defer SetHookTimeout(50 * time.Millisecond)()

	err = agent.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "go"})
	require.NoError(t, err, "loop should continue even if hook times out")
}

func TestCombineQueuedCalls_ManyCallsJoinedWithSeparator(t *testing.T) {
	t.Parallel()
	calls := []SessionAgentCall{
		{SessionID: "s1", Prompt: "first"},
		{SessionID: "s1", Prompt: "second"},
		{SessionID: "s1", Prompt: "third"},
	}
	out := combineQueuedCalls(calls)
	require.Equal(t, "first\n\nsecond\n\nthird", out.Prompt)
	require.Equal(t, "s1", out.SessionID)
}

func TestCombineQueuedCalls_PreservesRuntimePromptVisibility(t *testing.T) {
	t.Parallel()
	calls := []SessionAgentCall{
		{SessionID: "s1", Prompt: "job done", runtimePrompt: true},
		{SessionID: "s1", Prompt: "human follow-up"},
	}
	out := combineQueuedCalls(calls)
	require.Equal(t, "job done\n\nhuman follow-up", out.Prompt)
	require.Equal(t, "s1", out.SessionID)
	require.Len(t, out.turnPrompts, 2)
	require.Equal(t, turnPrompt{Text: "job done", Persist: true, Role: message.Runtime}, out.turnPrompts[0])
	require.Equal(t, turnPrompt{Text: "human follow-up", Persist: true, Role: message.User}, out.turnPrompts[1])
}

func TestCombineQueuedCalls_EmptyPrecondition(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() {
		_ = combineQueuedCalls(nil)
	})
}

func TestRun_RuntimePromptFeedsModelWithoutPersistingUserMessage(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "runtime prompt")
	require.NoError(t, err)

	inner := &scriptedModel{emits: []string{"exit"}}
	model := &streamCapturingModel{inner: inner}
	agent := testSessionAgent(env, model, model, "sys").(*sessionAgent)

	err = agent.Run(t.Context(), SessionAgentCall{
		SessionID:     sess.ID,
		Prompt:        "background job completed",
		runtimePrompt: true,
	})
	require.NoError(t, err)

	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	for _, msg := range msgs {
		require.NotEqual(t, message.User, msg.Role, "runtime prompt must not render as a user chat row")
	}
	require.Equal(t, message.Runtime, msgs[0].Role)
	require.Len(t, model.Captured(), 1)
	require.Equal(t, message.RuntimeText("background job completed"), fantasyMessageText(model.Captured()[0][1]))
}

func TestRun_JournalHintFollowsFirstUserPrompt(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "journal hint")
	require.NoError(t, err)

	inner := &scriptedModel{emits: []string{"exit"}}
	model := &streamCapturingModel{inner: inner}
	agent := testSessionAgent(env, model, model, "sys").(*sessionAgent)

	err = agent.Run(t.Context(), SessionAgentCall{
		SessionID:   sess.ID,
		Prompt:      "implement the thing",
		JournalPath: filepath.Join(t.TempDir(), "journal.md"),
	})
	require.NoError(t, err)

	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(msgs), 2)
	require.Equal(t, message.User, msgs[0].Role)
	require.Equal(t, "implement the thing", msgs[0].Content().Text)
	require.Equal(t, message.Runtime, msgs[1].Role)
	require.Contains(t, msgs[1].Content().Text, "session journal")

	require.Len(t, model.Captured(), 1)
	require.GreaterOrEqual(t, len(model.Captured()[0]), 3)
	require.Equal(t, "implement the thing", fantasyMessageText(model.Captured()[0][1]))
	require.Contains(t, fantasyMessageText(model.Captured()[0][2]), "session journal")
}

func TestRun_PostLoopDrainAllQueued(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "drain test")
	require.NoError(t, err)

	unblock := make(chan struct{})
	bm := &blockingModel{unblock: unblock}
	agent := testSessionAgent(env, bm, bm, "sys").(*sessionAgent)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	agent.activeRequests.Set(sess.ID, cancel)

	for _, prompt := range []string{"q1", "q2", "q3"} {
		err := agent.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: prompt})
		require.NoError(t, err)
	}
	require.Equal(t, 3, agent.QueuedPrompts(sess.ID), "3 prompts should queue")

	queued, ok := agent.messageQueue.Take(sess.ID)
	require.True(t, ok)
	require.Len(t, queued, 3)

	combined := combineQueuedCalls(queued)
	require.Equal(t, "q1\n\nq2\n\nq3", combined.Prompt)
	require.Equal(t, sess.ID, combined.SessionID)

	require.Equal(t, 0, agent.QueuedPrompts(sess.ID))

	agent.activeRequests.Del(sess.ID)
}

func TestQueuedPromptsHidesRuntimeOnlyQueue(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "runtime queue")
	require.NoError(t, err)

	agent := testSessionAgent(env, nil, nil, "sys").(*sessionAgent)
	agent.messageQueue.Set(sess.ID, []SessionAgentCall{
		{SessionID: sess.ID, Prompt: "background job completed", runtimePrompt: true},
	})

	require.Equal(t, 0, agent.QueuedPrompts(sess.ID))
	require.Empty(t, agent.QueuedPromptsList(sess.ID))

	queued, ok := agent.messageQueue.Get(sess.ID)
	require.True(t, ok)
	require.Len(t, queued, 1)
}

func TestAgent_Model_ReturnsPrimaryModel(t *testing.T) {
	t.Parallel()
	env := testEnv(t)

	lm := &mockLanguageModel{}
	sm := &mockLanguageModel{}

	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:   Model{Model: lm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		SmallModel:   Model{Model: sm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		PrimaryModel: Model{Model: lm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		SystemPrompt: "sys",
		Sessions:     env.sessions,
		Messages:     env.messages,
	})

	got := agent.Model()
	require.Equal(t, lm, got.Model, "Model() should return primaryModel (large)")
}

func TestAgent_Model_SwitchesToSmall(t *testing.T) {
	t.Parallel()
	env := testEnv(t)

	lm := &mockLanguageModel{}
	sm := &mockLanguageModel{}

	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:   Model{Model: lm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		SmallModel:   Model{Model: sm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		PrimaryModel: Model{Model: sm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
		SystemPrompt: "sys",
		Sessions:     env.sessions,
		Messages:     env.messages,
	})

	got := agent.Model()
	require.Equal(t, sm, got.Model, "Model() should return primaryModel (small)")
}

func TestRun_AssistantMessageUsesPrimaryModelConfigIDs(t *testing.T) {
	t.Parallel()
	env := testEnv(t)

	sess, err := env.sessions.Create(t.Context(), "model ids")
	require.NoError(t, err)

	model := &scriptedModel{
		emits:    []string{"exit"},
		modelID:  "fantasy-model-id",
		provider: "fantasy-provider",
	}
	primary := Model{
		Model:      model,
		CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 1024},
		ModelCfg:   config.SelectedModel{Provider: "config-provider", Model: "config-model"},
	}
	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:   primary,
		SmallModel:   primary,
		PrimaryModel: primary,
		SystemPrompt: "sys",
		Sessions:     env.sessions,
		Messages:     env.messages,
	}).(*sessionAgent)

	err = agent.Run(t.Context(), SessionAgentCall{
		SessionID: sess.ID,
		Prompt:    "go",
	})
	require.NoError(t, err)

	messages, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	var assistant message.Message
	for _, msg := range messages {
		if msg.Role == message.Assistant {
			assistant = msg
			break
		}
	}
	require.NotEmpty(t, assistant.ID)
	require.Equal(t, "config-model", assistant.Model)
	require.Equal(t, "config-provider", assistant.Provider)
}

func TestAgent_SetModels_UpdatesPrimaryModel(t *testing.T) {
	t.Parallel()
	env := testEnv(t)

	lm := &mockLanguageModel{}
	sm := &mockLanguageModel{}

	agent := testSessionAgent(env, lm, sm, "sys").(*sessionAgent)

	// Initially primary should match SetModels result (large by default based on ActiveTier)
	large := Model{Model: lm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}}
	small := Model{Model: sm, CatwalkCfg: catwalk.Model{ContextWindow: 200000}}
	agent.SetModels(large, small, small)

	require.Equal(t, small, agent.primaryModel.Get(), "primaryModel should be updated by SetModels")
	require.Equal(t, lm, agent.largeModel.Get().Model, "largeModel should be preserved")
	require.Equal(t, sm, agent.smallModel.Get().Model, "smallModel should be preserved")
}
