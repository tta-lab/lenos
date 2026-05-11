package agent

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	bm := &scriptedModel{emits: []string{"echo hi", "exit"}}
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

	bm := &scriptedModel{emits: []string{"echo one", "echo two", "exit"}}
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

func TestRun_AutoCompactBeforeFirstStepRunsBeforePersistingCurrentPrompt(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "auto-compact before first step test")
	require.NoError(t, err)

	model := &scriptedModel{
		emits: []string{"summary text", "exit"},
	}

	agent := testSessionAgent(env, model, model, "sys").(*sessionAgent)
	testModel := Model{
		Model: model,
		CatwalkCfg: catwalk.Model{
			ContextWindow:    100,
			DefaultMaxTokens: 1024,
		},
	}
	agent.SetModels(testModel, testModel, testModel)

	_, err = env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "old request"}},
	})
	require.NoError(t, err)
	_, err = env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: []message.ContentPart{message.TextContent{Text: "old response"}},
	})
	require.NoError(t, err)

	sess.PromptTokens = 81
	sess.CompletionTokens = 0
	sess, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)

	err = agent.Run(t.Context(), SessionAgentCall{
		SessionID: sess.ID,
		Prompt:    "new request",
	})
	require.NoError(t, err)

	persisted, err := env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, persisted.SummaryMessageID, "auto-compact should have set SummaryMessageID")

	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	summaryIndex := -1
	currentPromptIndex := -1
	for i, m := range msgs {
		switch {
		case m.ID == persisted.SummaryMessageID:
			summaryIndex = i
		case m.Role == message.User && m.Content().Text == "new request":
			currentPromptIndex = i
		}
		assert.NotContains(t, m.Content().Text, "previous session was interrupted because it got too long")
	}
	require.NotEqual(t, -1, summaryIndex, "summary message should be present")
	require.NotEqual(t, -1, currentPromptIndex, "current prompt should be persisted")
	assert.Less(t, summaryIndex, currentPromptIndex, "pre-turn compact should happen before persisting the current prompt")
}

func TestRun_AutoCompactWaitsUntilNextStepAfterBashResult(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "auto-compact after result test")
	require.NoError(t, err)

	model := &scriptedModel{
		emits: []string{"printf 'ok\\n'", "summary text", "exit"},
		usages: []fantasy.Usage{
			{InputTokens: 81, OutputTokens: 0},
			{InputTokens: 0, OutputTokens: 0},
			{InputTokens: 0, OutputTokens: 0},
		},
	}

	agent := testSessionAgent(env, model, model, "sys").(*sessionAgent)
	testModel := Model{
		Model: model,
		CatwalkCfg: catwalk.Model{
			ContextWindow:    100,
			DefaultMaxTokens: 1024,
		},
	}
	agent.SetModels(testModel, testModel, testModel)

	err = agent.Run(t.Context(), SessionAgentCall{
		SessionID: sess.ID,
		Prompt:    "go",
	})
	require.NoError(t, err)

	persisted, err := env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.NotEmpty(t, persisted.SummaryMessageID, "auto-compact should have set SummaryMessageID")

	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	resultIndex := -1
	summaryIndex := -1
	for i, m := range msgs {
		if m.ID == persisted.SummaryMessageID {
			summaryIndex = i
		}
		if m.Role == message.Result && strings.Contains(m.CommandContent().Command, "printf 'ok\\n'") {
			resultIndex = i
		}
	}
	require.NotEqual(t, -1, resultIndex, "bash result should be persisted before compact")
	require.NotEqual(t, -1, summaryIndex, "summary message should be present")
	assert.Less(t, resultIndex, summaryIndex, "compact should run only at the next pre-step boundary")
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
		SessionID: sess.ID,
		Prompt:    "go",
	})
	require.Error(t, err, "Summarize failure must propagate from Run (parity with pre-bash-first behavior)")

	persisted, gerr := env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, gerr)
	assert.Empty(t, persisted.SummaryMessageID, "no summary should be set on Summarize failure")
}

func TestRun_AutoCompactSummarizeSucceedsButReentryFails(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "auto-compact reentry-fail test")
	require.NoError(t, err)

	// Call sequence:
	// [0] "trigger" — usage crosses threshold, then the bash result is persisted.
	// [1] "summary" — consumed by the first Summarize().
	// [2] "do work" — re-entry crosses the threshold after its bash result.
	// [3] "err" — errOn=[3] makes the second Summarize() fail.
	model := &scriptedModel{
		emits: []string{"trigger", "summary", "do work", "err"},
		usages: []fantasy.Usage{
			{InputTokens: 200_000, OutputTokens: 0},
			{InputTokens: 0, OutputTokens: 0},
			{InputTokens: 200_000, OutputTokens: 0},
			{InputTokens: 0, OutputTokens: 0},
		},
		errOn: []int{3}, // error on 4th stream call (re-entry compact-trigger's next step)
	}

	agent := testSessionAgent(env, model, model, "sys").(*sessionAgent)
	agent.largeModel.Set(Model{
		Model:      model,
		CatwalkCfg: catwalk.Model{ContextWindow: 200_001, DefaultMaxTokens: 1024},
	})

	err = agent.Run(t.Context(), SessionAgentCall{
		SessionID: sess.ID,
		Prompt:    "go",
	})
	require.Error(t, err, "second Summarize error should propagate from Run")

	persisted, gerr := env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, gerr)
	assert.NotEmpty(t, persisted.SummaryMessageID, "summary should be set before re-entry error")
}

func TestRun_DisableAutoSummarizePreventsCompact(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "disable auto-summarize test")
	require.NoError(t, err)

	model := &scriptedModel{
		emits: []string{"exit"},
		usages: []fantasy.Usage{
			{InputTokens: 200_000, OutputTokens: 0},
		},
	}

	agent := testSessionAgent(env, model, model, "sys").(*sessionAgent)
	agent.largeModel.Set(Model{
		Model:      model,
		CatwalkCfg: catwalk.Model{ContextWindow: 200_001, DefaultMaxTokens: 1024},
	})
	// Disable auto-summarize even though token count would trigger it.
	agent.disableAutoSummarize = true

	err = agent.Run(t.Context(), SessionAgentCall{
		SessionID: sess.ID,
		Prompt:    "go",
	})
	require.NoError(t, err)

	persisted, gerr := env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, gerr)
	assert.Empty(t, persisted.SummaryMessageID, "no summary should be set when disableAutoSummarize is true")
}

func TestGetSessionMessagesAfterSummaryKeepsRecentUserMessages(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "recent user retention test")
	require.NoError(t, err)

	for _, text := range []string{"u1", "u2", "u3", "u4"} {
		_, err = env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: text}},
		})
		require.NoError(t, err)
		_, err = env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
			Role:  message.Assistant,
			Parts: []message.ContentPart{message.TextContent{Text: "answer " + text}},
		})
		require.NoError(t, err)
	}
	_, err = env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: autoCompactContinuationPrompt("u4")}},
	})
	require.NoError(t, err)

	summary, err := env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
		Role:             message.Assistant,
		Parts:            []message.ContentPart{message.TextContent{Text: "summary"}},
		IsSummaryMessage: true,
	})
	require.NoError(t, err)
	sess.SummaryMessageID = summary.ID
	sess, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)

	agent := testSessionAgent(env, &mockLanguageModel{}, &mockLanguageModel{}, "sys").(*sessionAgent)
	got, err := agent.getSessionMessages(t.Context(), sess)
	require.NoError(t, err)

	require.Len(t, got, 4)
	require.Equal(t, "summary", got[0].Content().Text)
	require.Equal(t, message.User, got[0].Role, "summary should still enter model context as a user message")
	require.Equal(t, "u2", got[1].Content().Text)
	require.Equal(t, "u3", got[2].Content().Text)
	require.Equal(t, "u4", got[3].Content().Text)
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

func TestAgent_Summarize_UsesPrimaryModelAndConfigIDs(t *testing.T) {
	t.Parallel()
	env := testEnv(t)

	sess, err := env.sessions.Create(t.Context(), "summarize test")
	require.NoError(t, err)

	largeModel := &scriptedModel{
		emits:    []string{"large summary"},
		modelID:  "gpt-5.5",
		provider: "openai",
	}
	smallModel := &scriptedModel{
		emits:    []string{"small summary"},
		usages:   []fantasy.Usage{{InputTokens: 100, OutputTokens: 50}},
		modelID:  "deepseek-v4-flash",
		provider: "openai",
	}

	large := Model{
		Model:      largeModel,
		CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 1024},
		ModelCfg:   config.SelectedModel{Provider: "openai-main", Model: "gpt-5.5"},
	}
	small := Model{
		Model:      smallModel,
		CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 1024},
		ModelCfg:   config.SelectedModel{Provider: "deepseek-main", Model: "deepseek-v4-flash"},
	}
	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:   large,
		SmallModel:   small,
		PrimaryModel: small,
		SystemPrompt: "sys",
		Sessions:     env.sessions,
		Messages:     env.messages,
	}).(*sessionAgent)

	require.Equal(t, smallModel, agent.primaryModel.Get().Model, "sanity: primary should be small")

	// Add a user message so Summarize has something to summarize
	_, err = env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "hello world"}},
	})
	require.NoError(t, err)

	// Only the assistant message will exist — Summarize reads history from messages.
	_, err = env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: []message.ContentPart{message.TextContent{Text: "hi back"}},
		Model: "test-model",
	})
	require.NoError(t, err)

	err = agent.Summarize(t.Context(), sess.ID, fantasy.ProviderOptions{})
	require.NoError(t, err)

	require.Equal(t, 0, largeModel.calls, "largeModel should not be called by Summarize")
	require.Equal(t, 1, smallModel.calls, "primary smallModel should be called by Summarize")

	updated, err := env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.NotEmpty(t, updated.SummaryMessageID)

	summaryMsg, err := env.messages.Get(t.Context(), updated.SummaryMessageID)
	require.NoError(t, err)
	require.Equal(t, "small summary", summaryMsg.Content().Text)
	require.Equal(t, "deepseek-v4-flash", summaryMsg.Model)
	require.Equal(t, "deepseek-main", summaryMsg.Provider)
	require.True(t, summaryMsg.IsSummaryMessage)

	// Also verify primary model is still small (invariant: Summarize doesn't change primary)
	require.Equal(t, smallModel, agent.primaryModel.Get().Model, "primary should still be small after Summarize")
}

func TestAgent_Summarize_UsesNormalSystemPromptAndFinalCompactUserInstruction(t *testing.T) {
	t.Parallel()
	env := testEnv(t)

	sess, err := env.sessions.Create(t.Context(), "summarize prompt shape")
	require.NoError(t, err)

	inner := &scriptedModel{
		emits: []string{"narrate <<'LENOS_CONTEXT_COMPACTION'\nSummary\nLENOS_CONTEXT_COMPACTION"},
	}
	model := &streamCapturingModel{inner: inner}
	primary := Model{
		Model:      model,
		CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 1024},
		ModelCfg:   config.SelectedModel{Provider: "config-provider", Model: "config-model"},
	}
	agent := NewSessionAgent(SessionAgentOptions{
		LargeModel:   primary,
		SmallModel:   primary,
		PrimaryModel: primary,
		SystemPrompt: "NORMAL BASH SYSTEM",
		Sessions:     env.sessions,
		Messages:     env.messages,
	}).(*sessionAgent)

	_, err = env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "continue the work"}},
	})
	require.NoError(t, err)

	err = agent.Summarize(t.Context(), sess.ID, fantasy.ProviderOptions{})
	require.NoError(t, err)

	require.Len(t, model.captured, 1)
	prompt := model.captured[0]
	require.GreaterOrEqual(t, len(prompt), 3)

	first := prompt[0]
	require.Equal(t, fantasy.MessageRoleSystem, first.Role)
	firstText := fantasyMessageText(first)
	require.Equal(t, "NORMAL BASH SYSTEM", firstText)
	require.NotContains(t, firstText, "LENOS_CONTEXT_COMPACTION")

	last := prompt[len(prompt)-1]
	require.Equal(t, fantasy.MessageRoleUser, last.Role)
	lastText := fantasyMessageText(last)
	require.Contains(t, lastText, summaryInstructionsPrompt())
	require.Contains(t, lastText, formatSummaryPrompt(nil))
	require.Contains(t, lastText, summaryOutputProtocolPrompt())
	require.Contains(t, lastText, "narrate <<'LENOS_CONTEXT_COMPACTION'")

	systemMessages := 0
	for _, msg := range prompt[:len(prompt)-1] {
		if msg.Role != fantasy.MessageRoleSystem {
			continue
		}
		systemMessages++
		systemText := fantasyMessageText(msg)
		require.NotContains(t, systemText, "LENOS_CONTEXT_COMPACTION")
	}
	require.Equal(t, 1, systemMessages)
}

func TestAgent_Summarize_RetriesRetryableStreamEOFAndClearsPartialSummary(t *testing.T) {
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "summarize retry test")
	require.NoError(t, err)

	_, err = env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "hello world"}},
	})
	require.NoError(t, err)

	model := &retryableErrorThenSuccessModel{
		firstDelta: "partial stale summary",
		secondEmit: "fresh summary",
	}
	agent := testSessionAgent(env, model, &mockLanguageModel{}, "sys").(*sessionAgent)

	err = agent.Summarize(t.Context(), sess.ID, fantasy.ProviderOptions{})
	require.NoError(t, err)
	assert.Equal(t, 2, model.attempts)

	updated, err := env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.NotEmpty(t, updated.SummaryMessageID)

	summaryMsg, err := env.messages.Get(t.Context(), updated.SummaryMessageID)
	require.NoError(t, err)
	assert.Equal(t, "fresh summary", summaryMsg.Content().Text)
	assert.NotContains(t, summaryMsg.Content().Text, "partial stale summary")
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
