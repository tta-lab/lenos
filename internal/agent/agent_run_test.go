package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func (m *mockMessageService) Delete(_ context.Context, _ string) error                { return nil }
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

	usage := fantasy.Usage{
		InputTokens:  1000,
		OutputTokens: 500,
	}

	updated, ok := agent.saveSessionUsage(t.Context(), sess.ID, usage, nil, "save failed")
	require.True(t, ok, "saveSessionUsage should succeed")
	assert.Equal(t, int64(1000), updated.PromptTokens, "PromptTokens should reflect InputTokens")
	assert.Equal(t, int64(500), updated.CompletionTokens, "CompletionTokens should reflect OutputTokens")
	assert.Greater(t, updated.Cost, 0.0, "Cost should be non-zero")

	persisted, err := env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), persisted.PromptTokens)
	assert.Equal(t, int64(500), persisted.CompletionTokens)
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
		SessionID:  sess.ID,
		Prompt:     "second",
		ProviderID: "test",
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
		SessionID:  "s1",
		Prompt:     "hello",
		ProviderID: "test",
	}}
	out := combineQueuedCalls(calls)
	require.Equal(t, "hello", out.Prompt)
	require.Equal(t, "s1", out.SessionID)
	require.Equal(t, "test", out.ProviderID)
}

func TestCombineQueuedCalls_ManyCallsJoinedWithSeparator(t *testing.T) {
	t.Parallel()
	calls := []SessionAgentCall{
		{SessionID: "s1", Prompt: "first", ProviderID: "p1"},
		{SessionID: "s1", Prompt: "second", ProviderID: "p2"},
		{SessionID: "s1", Prompt: "third", ProviderID: "p3"},
	}
	out := combineQueuedCalls(calls)
	require.Equal(t, "first\n\nsecond\n\nthird", out.Prompt)
	require.Equal(t, "p1", out.ProviderID, "first call provider preserved")
	require.Equal(t, "s1", out.SessionID)
}

func TestCombineQueuedCalls_EmptyPrecondition(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() {
		_ = combineQueuedCalls(nil)
	})
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
		err := agent.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: prompt, ProviderID: "test"})
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

func TestRun_AutoCompactFiresAndReentersWithPrefix(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "auto-compact e2e test")
	require.NoError(t, err)

	model := &scriptedModel{
		emits: []string{"do nothing", "summary text", "exit"},
		usages: []fantasy.Usage{
			{InputTokens: 200_000, OutputTokens: 0},
			{InputTokens: 0, OutputTokens: 0},
			{InputTokens: 0, OutputTokens: 0},
		},
	}

	agent := testSessionAgent(env, model, model, "sys").(*sessionAgent)
	agent.largeModel.Set(Model{
		Model: model,
		CatwalkCfg: catwalk.Model{
			ContextWindow:    200_001,
			DefaultMaxTokens: 1024,
		},
	})

	err = agent.Run(t.Context(), SessionAgentCall{
		SessionID:  sess.ID,
		Prompt:     "go",
		ProviderID: "test",
	})
	require.NoError(t, err)

	persisted, err := env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, persisted.SummaryMessageID, "auto-compact should have set SummaryMessageID")

	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	var sawPrefix bool
	for _, m := range msgs {
		if m.Role != message.User {
			continue
		}
		if strings.Contains(m.Content().Text, "previous session was interrupted because it got too long") {
			sawPrefix = true
			break
		}
	}
	assert.True(t, sawPrefix, "post-compact re-entry should record the interruption-prefixed user prompt")
}

func TestRun_AutoCompactSummarizeErrorPropagates(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "auto-compact summarize-fail test")
	require.NoError(t, err)

	model := &scriptedModel{
		emits: []string{"trigger compact", "summary"},
		usages: []fantasy.Usage{
			{InputTokens: 200_000, OutputTokens: 0},
			{InputTokens: 0, OutputTokens: 0},
		},
		errOn: []int{1},
	}

	agent := testSessionAgent(env, model, model, "sys").(*sessionAgent)
	agent.largeModel.Set(Model{
		Model:      model,
		CatwalkCfg: catwalk.Model{ContextWindow: 200_001, DefaultMaxTokens: 1024},
	})

	err = agent.Run(t.Context(), SessionAgentCall{
		SessionID:  sess.ID,
		Prompt:     "go",
		ProviderID: "test",
	})
	require.Error(t, err, "Summarize failure must propagate from Run (parity with pre-bash-first behavior)")

	persisted, gerr := env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, gerr)
	assert.Empty(t, persisted.SummaryMessageID, "no summary should be set on Summarize failure")
}
