package agent

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"charm.land/fantasy"

	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/pubsub"
)

// =============================================================================
// Test infrastructure for loop integration tests
// Ported from deleted loop_test.go / agent_run_test.go
// =============================================================================

// scriptedModel returns canned emits via Stream(). Each call consumes one
// entry; calls past the last entry panic.
type scriptedModel struct {
	mu       sync.Mutex
	emits    []string
	usages   []fantasy.Usage
	errOn    []int // call indices where Stream yields an error
	calls    int
	modelID  string
	provider string
}

func (m *scriptedModel) Model() string {
	if m.modelID != "" {
		return m.modelID
	}
	return "test-model"
}

func (m *scriptedModel) Provider() string {
	if m.provider != "" {
		return m.provider
	}
	return "test-provider"
}

func (m *scriptedModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	panic("not used")
}

func (m *scriptedModel) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls >= len(m.emits) {
		panic("scriptedModel: ran out of canned emits")
	}
	for _, idx := range m.errOn {
		if m.calls == idx {
			m.calls++
			return func(yield func(fantasy.StreamPart) bool) {
				yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: errors.New("scripted error")})
			}, nil
		}
	}
	out := m.emits[m.calls]
	u := fantasy.Usage{InputTokens: 1, OutputTokens: 1}
	if m.calls < len(m.usages) {
		u = m.usages[m.calls]
	}
	m.calls++
	return func(yield func(fantasy.StreamPart) bool) {
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: out}) {
			return
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, Usage: u})
	}, nil
}

func (m *scriptedModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	panic("not used")
}

func (m *scriptedModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	panic("not used")
}

var _ fantasy.LanguageModel = (*scriptedModel)(nil)

// retryableErrorThenSuccessModel streams a partial delta then an error on the
// first call, then a full delta + finish on the second.
type retryableErrorThenSuccessModel struct {
	attempts   int
	firstDelta string
	secondEmit string
}

func (m *retryableErrorThenSuccessModel) Model() string    { return "retry-model" }
func (m *retryableErrorThenSuccessModel) Provider() string { return "test-provider" }
func (m *retryableErrorThenSuccessModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	panic("not used")
}

func (m *retryableErrorThenSuccessModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	m.attempts++
	if m.attempts == 1 {
		return func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: m.firstDelta}) {
				return
			}
			yield(fantasy.StreamPart{
				Type: fantasy.StreamPartTypeError,
				Error: &fantasy.ProviderError{
					Title:   "stream transport error",
					Message: "unexpected EOF",
					Cause:   io.ErrUnexpectedEOF,
				},
			})
		}, nil
	}
	return func(yield func(fantasy.StreamPart) bool) {
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: m.secondEmit}) {
			return
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, Usage: fantasy.Usage{InputTokens: 2, OutputTokens: 3}})
	}, nil
}

func (m *retryableErrorThenSuccessModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	panic("not used")
}

func (m *retryableErrorThenSuccessModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	panic("not used")
}

var _ fantasy.LanguageModel = (*retryableErrorThenSuccessModel)(nil)

// streamCapturingModel wraps a LanguageModel and records prompt messages
// sent to each Stream() call. Thread-safe for parallel tests.
type streamCapturingModel struct {
	inner    fantasy.LanguageModel
	captured [][]fantasy.Message
	mu       sync.Mutex
}

func (c *streamCapturingModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	c.mu.Lock()
	c.captured = append(c.captured, append([]fantasy.Message(nil), call.Prompt...))
	c.mu.Unlock()
	return c.inner.Stream(ctx, call)
}

func (c *streamCapturingModel) Captured() [][]fantasy.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]fantasy.Message, len(c.captured))
	for i, p := range c.captured {
		out[i] = append([]fantasy.Message(nil), p...)
	}
	return out
}

func (c *streamCapturingModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	return c.inner.Generate(ctx, call)
}

func (c *streamCapturingModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return c.inner.GenerateObject(ctx, call)
}

func (c *streamCapturingModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return c.inner.StreamObject(ctx, call)
}

func (c *streamCapturingModel) Provider() string { return c.inner.Provider() }
func (c *streamCapturingModel) Model() string    { return c.inner.Model() }

var _ fantasy.LanguageModel = (*streamCapturingModel)(nil)

// fakeRunner returns canned ExecResults in order. Tests drive the classify=exec
// branch without touching /bin/bash.
type fakeRunner struct {
	mu      sync.Mutex
	results []ExecResult
	calls   int
	bash    []string
}

func (r *fakeRunner) Run(_ context.Context, bash string, _ map[string]string, _ []AllowedPath) ExecResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bash = append(r.bash, bash)
	if r.calls >= len(r.results) {
		panic("fakeRunner: ran out of canned results")
	}
	out := r.results[r.calls]
	r.calls++
	return out
}

// =============================================================================
// Helpers
// =============================================================================

// mockMessageService is an in-memory message.Service for unit tests.
type mockMessageService struct {
	mu       sync.Mutex
	messages map[string]message.Message
	order    []string
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
	m.updates = append(m.updates, msg.Clone())
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

func (m *mockMessageService) DeleteSessionMessages(_ context.Context, _ string) error {
	return nil
}

func resultsByOrder(ms *mockMessageService) []message.Message {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	out := make([]message.Message, 0, len(ms.order))
	for _, id := range ms.order {
		msg := ms.messages[id]
		if msg.Role == message.Result {
			out = append(out, msg)
		}
	}
	return out
}

func newLoopDeps(t *testing.T, model fantasy.LanguageModel, runner Runner, drain func() []turnPrompt) (loopDeps, *mockMessageService) {
	t.Helper()
	ms := newMockMessageService()
	return loopDeps{
		model:      Model{Model: model},
		messages:   ms,
		runner:     runner,
		sessionID:  "s-test",
		sysPrompt:  "you are a test",
		drainQueue: drain,
	}, ms
}

func cannedDrainer(rounds ...[]string) func() []turnPrompt {
	i := 0
	return func() []turnPrompt {
		if i >= len(rounds) {
			return nil
		}
		out := make([]turnPrompt, len(rounds[i]))
		for j, prompt := range rounds[i] {
			out[j] = turnPrompt{Text: prompt, Persist: true}
		}
		i++
		return out
	}
}

func noDrain() []turnPrompt { return nil }
