package agent

import (
	"context"
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
