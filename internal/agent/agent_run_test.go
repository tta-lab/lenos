package agent

import (
	"context"
	"sync"
	"testing"

	"charm.land/fantasy"
	"github.com/tta-lab/lenos/internal/pubsub"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/logos/v2"
)

// mockMessageService is a minimal in-memory message.Service for unit testing runState handlers.
type mockMessageService struct {
	mu       sync.Mutex
	messages map[string]message.Message
	order    []string // insertion order for deterministic iteration
	idSeq    int
}

func newMockMessageService() *mockMessageService {
	return &mockMessageService{messages: make(map[string]message.Message)}
}

func (m *mockMessageService) Subscribe(ctx context.Context) <-chan pubsub.Event[message.Message] {
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
	return nil, nil
}

func (m *mockMessageService) ListUserMessages(_ context.Context, _ string) ([]message.Message, error) {
	return nil, nil
}

func (m *mockMessageService) ListAllUserMessages(context.Context) ([]message.Message, error) {
	return nil, nil
}

func (m *mockMessageService) Delete(_ context.Context, _ string) error                { return nil }
func (m *mockMessageService) DeleteSessionMessages(_ context.Context, _ string) error { return nil }

// mockLanguageModel implements fantasy.LanguageModel for test construction.
// All methods panic since they are never called in handler unit tests.
type mockLanguageModel struct{}

func (m *mockLanguageModel) Model() string    { return "test-model" }
func (m *mockLanguageModel) Provider() string { return "test-provider" }
func (m *mockLanguageModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	panic("not implemented")
}

func (m *mockLanguageModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	panic("not implemented")
}

func (m *mockLanguageModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	panic("not implemented")
}

func (m *mockLanguageModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	panic("not implemented")
}

// mockProvider implements fantasy.Provider for test construction.
type mockProvider struct{}

func (p *mockProvider) Name() string {
	return "test-provider"
}

func (p *mockProvider) LanguageModel(ctx context.Context, modelID string) (fantasy.LanguageModel, error) {
	return &mockLanguageModel{}, nil
}

var _ fantasy.Provider = (*mockProvider)(nil)

// --- Tests ---

func TestRunState_StepStart_CreatesPlaceholder(t *testing.T) {
	t.Parallel()
	ms := newMockMessageService()
	state := &runState{
		sessionID: "s1",
		logosCfg:  logos.Config{Model: "test-model", Provider: &mockProvider{}},
		messages:  ms,
		ctx:       context.Background(),
	}

	state.handleStepStart(0)

	require.NotNil(t, state.currentAssistant)
	assert.Equal(t, message.Assistant, state.currentAssistant.Role)
	assert.Equal(t, "test-model", state.currentAssistant.Model)
	assert.Equal(t, "test-provider", state.currentAssistant.Provider)
	// Placeholder has empty text part
	parts := state.currentAssistant.Parts
	require.Len(t, parts, 1)
	tc, ok := parts[0].(message.TextContent)
	require.True(t, ok)
	assert.Equal(t, "", tc.Text)
}

func TestRunState_HandleDelta_AppendsProse(t *testing.T) {
	t.Parallel()
	ms := newMockMessageService()
	state := &runState{
		sessionID: "s1",
		messages:  ms,
		ctx:       context.Background(),
	}
	state.handleStepStart(0)

	state.handleDelta("Hello ")
	state.handleDelta("world")

	// Should have appended to the placeholder.
	updated, err := ms.Get(context.Background(), state.currentAssistant.ID)
	require.NoError(t, err)
	assert.Equal(t, "Hello world", updated.Content().Text)
}

func TestRunState_HandleDelta_CmdChunk_AppendsAndCreatesPending(t *testing.T) {
	t.Parallel()
	ms := newMockMessageService()
	state := &runState{
		sessionID: "s1",
		messages:  ms,
		ctx:       context.Background(),
	}
	state.handleStepStart(0)

	state.handleDelta("<cmd>echo hello</cmd>")

	// Assistant got the verbatim chunk.
	updated, err := ms.Get(context.Background(), state.currentAssistant.ID)
	require.NoError(t, err)
	assert.Contains(t, updated.Content().Text, "<cmd>echo hello</cmd>")

	// pendingResult was created.
	require.NotNil(t, state.pendingResult)
	cmdContent := state.pendingResult.CommandContent()
	assert.Equal(t, "echo hello", cmdContent.Command)
	assert.True(t, cmdContent.Pending)
}

func TestRunState_HandleDelta_CmdChunk_EmptyCmd_NoOp(t *testing.T) {
	t.Parallel()
	ms := newMockMessageService()
	state := &runState{
		sessionID: "s1",
		messages:  ms,
		ctx:       context.Background(),
	}
	state.handleStepStart(0)

	state.handleDelta("<cmd></cmd>")

	// No pending result created.
	assert.Nil(t, state.pendingResult)
	// Nothing appended to assistant.
	updated, err := ms.Get(context.Background(), state.currentAssistant.ID)
	require.NoError(t, err)
	assert.Equal(t, "", updated.Content().Text)
}

func TestRunState_HandleDelta_WhitespaceFineNoGuard(t *testing.T) {
	t.Parallel()
	ms := newMockMessageService()
	state := &runState{
		sessionID: "s1",
		messages:  ms,
		ctx:       context.Background(),
	}
	state.handleStepStart(0)

	// Whitespace-only delta should not panic and should append harmlessly.
	state.handleDelta("\n")
	state.handleDelta("  ")

	updated, err := ms.Get(context.Background(), state.currentAssistant.ID)
	require.NoError(t, err)
	assert.Equal(t, "\n  ", updated.Content().Text)
}

func TestRunState_HandleReasoningDelta_StreamsLive(t *testing.T) {
	t.Parallel()
	ms := newMockMessageService()
	state := &runState{
		sessionID: "s1",
		messages:  ms,
		ctx:       context.Background(),
	}
	state.handleStepStart(0)

	state.handleReasoningDelta("Thinking step 1...")
	state.handleReasoningDelta(" More thoughts.")

	updated, err := ms.Get(context.Background(), state.currentAssistant.ID)
	require.NoError(t, err)
	assert.Equal(t, "Thinking step 1... More thoughts.", updated.ReasoningContent().Thinking)
}

func TestRunState_HandleReasoningSignature_SetsOnce(t *testing.T) {
	t.Parallel()
	ms := newMockMessageService()
	state := &runState{
		sessionID: "s1",
		messages:  ms,
		ctx:       context.Background(),
	}
	state.handleStepStart(0)

	state.handleReasoningSignature("sig-abc123")

	updated, err := ms.Get(context.Background(), state.currentAssistant.ID)
	require.NoError(t, err)
	assert.Equal(t, "sig-abc123", updated.ReasoningContent().Signature)
	assert.NotZero(t, updated.ReasoningContent().FinishedAt) // FinishThinking was called
}

func TestRunState_HandleCommandResult_UpdatesPending(t *testing.T) {
	t.Parallel()
	ms := newMockMessageService()
	state := &runState{
		sessionID: "s1",
		messages:  ms,
		ctx:       context.Background(),
	}
	state.handleStepStart(0)
	state.handleDelta("<cmd>echo hello</cmd>")
	pendingID := state.pendingResult.ID

	state.handleCommandResult("echo hello", "hello world\n", 0)

	// pendingResult should be cleared.
	assert.Nil(t, state.pendingResult)

	// Result message should be updated.
	updated, err := ms.Get(context.Background(), pendingID)
	require.NoError(t, err)
	cmdContent := updated.CommandContent()
	assert.Equal(t, "echo hello", cmdContent.Command)
	assert.Equal(t, "hello world\n", cmdContent.Output)
	assert.False(t, cmdContent.Pending)
}

func TestRunState_HandleCommandResult_NoPending_Fallback(t *testing.T) {
	t.Parallel()
	ms := newMockMessageService()
	state := &runState{
		sessionID: "s1",
		messages:  ms,
		ctx:       context.Background(),
	}
	// No pending result set.

	state.handleCommandResult("echo bye", "bye!\n", 0)

	// Should have created a fallback result.
	// Fallback created a result message in the store.
	var found message.Message
	for _, msg := range ms.messages {
		if msg.Role == message.Result {
			found = msg
			break
		}
	}
	require.NotEqual(t, "", found.ID, "fallback result should be created")
}

func TestRunState_HandleCommandResult_BlockedCmd_ExitNeg2(t *testing.T) {
	t.Parallel()
	ms := newMockMessageService()
	state := &runState{
		sessionID: "s1",
		logosCfg:  logos.Config{Model: "test-model", Provider: &mockProvider{}},
		messages:  ms,
		ctx:       context.Background(),
	}
	state.handleStepStart(0)
	state.handleDelta("<cmd>forbidden-cmd</cmd>")

	state.handleCommandResult("forbidden-cmd", "Command not allowed: forbidden-cmd", -2)

	// After handleCommandResult, pendingResult is cleared.
	assert.Nil(t, state.pendingResult, "pendingResult should be cleared after handleCommandResult")
	// Verify the result was persisted in the store.
	var found message.Message
	for _, msg := range ms.messages {
		if msg.Role == message.Result {
			found = msg
			break
		}
	}
	require.NotEqual(t, "", found.ID, "result should be created")
	cmdContent := found.CommandContent()
	assert.Equal(t, "forbidden-cmd", cmdContent.Command)
	assert.Equal(t, "Command not allowed: forbidden-cmd", cmdContent.Output)
	assert.Equal(t, -2, *cmdContent.ExitCode)
	assert.False(t, cmdContent.Pending)
}

func TestRunState_HandleStepEnd_SetsDefaultToolUseFinish(t *testing.T) {
	t.Parallel()
	ms := newMockMessageService()
	state := &runState{
		sessionID: "s1",
		messages:  ms,
		ctx:       context.Background(),
	}
	state.handleStepStart(0)

	state.handleStepEnd(0)

	assert.True(t, state.currentAssistant.IsFinished())
	assert.Equal(t, message.FinishReasonToolUse, state.currentAssistant.FinishReason())
}

func TestRunState_HandleStepEnd_NoOpIfAlreadyFinished(t *testing.T) {
	t.Parallel()
	ms := newMockMessageService()
	state := &runState{
		sessionID: "s1",
		messages:  ms,
		ctx:       context.Background(),
	}
	state.handleStepStart(0)
	state.handleStepEnd(0) // sets FinishReasonToolUse
	finish1 := state.currentAssistant.FinishReason()

	state.handleStepEnd(0) // should be idempotent

	assert.Equal(t, message.FinishReasonToolUse, finish1)
	assert.Equal(t, message.FinishReasonToolUse, state.currentAssistant.FinishReason())
}

func TestRunState_HandleTurnEnd_OverridesFinishFromStopReason(t *testing.T) {
	t.Parallel()
	tests := []struct {
		reason logos.StopReason
		want   message.FinishReason
	}{
		{logos.StopReasonFinal, message.FinishReasonEndTurn},
		{logos.StopReasonCanceled, message.FinishReasonCanceled},
		{logos.StopReasonError, message.FinishReasonError},
		{logos.StopReasonHallucinationLimit, message.FinishReasonError},
		{logos.StopReasonMaxSteps, message.FinishReasonError},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(string(tc.reason), func(t *testing.T) {
			t.Parallel()
			ms := newMockMessageService()
			s := &runState{
				sessionID: "s1",
				messages:  ms,
				ctx:       context.Background(),
			}
			s.handleStepStart(0)
			s.handleStepEnd(0) // sets FinishReasonToolUse
			beforeID := s.currentAssistant.ID

			s.handleTurnEnd(tc.reason)

			// currentAssistant should be cleared.
			assert.Nil(t, s.currentAssistant)
			// The message was updated to the terminal reason.
			updated, err := ms.Get(context.Background(), beforeID)
			require.NoError(t, err)
			assert.Equal(t, tc.want, updated.FinishReason())
		})
	}
}

func TestRunState_HandleTurnEnd_AbandonsPendingResultOnCancel(t *testing.T) {
	t.Parallel()
	ms := newMockMessageService()
	state := &runState{
		sessionID: "s1",
		messages:  ms,
		ctx:       context.Background(),
	}
	state.handleStepStart(0)
	state.handleDelta("<cmd>sleep 10</cmd>")
	pendingID := state.pendingResult.ID

	state.handleTurnEnd(logos.StopReasonCanceled)

	// pendingResult should be cleared.
	assert.Nil(t, state.pendingResult)

	// Abandoned result should be persisted.
	updated, err := ms.Get(context.Background(), pendingID)
	require.NoError(t, err)
	cmdContent := updated.CommandContent()
	assert.Contains(t, cmdContent.Output, "canceled")
}

func TestRunState_ReasoningLivesInOwningStep(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		steps []struct {
			reasoning []string
			prose     []string
		}
		wantReasoning []string
	}{
		{
			name: "step0 has reasoning A plus text a, step1 has reasoning B plus text b, step2 has text c only",
			steps: []struct {
				reasoning []string
				prose     []string
			}{
				{reasoning: []string{"Thinking A"}, prose: []string{"text a"}},
				{reasoning: []string{"Thinking B"}, prose: []string{"text b"}},
				{prose: []string{"text c"}},
			},
			wantReasoning: []string{"Thinking A", "Thinking B", ""},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ms := newMockMessageService()
			state := &runState{
				sessionID: "s1",
				messages:  ms,
				ctx:       context.Background(),
			}

			for i, step := range tc.steps {
				state.handleStepStart(i)
				for _, r := range step.reasoning {
					state.handleReasoningDelta(r)
				}
				for _, p := range step.prose {
					state.handleDelta(p)
				}
				state.handleStepEnd(i)
			}

			// Verify reasoning lives in the owning step.
			// createdAssistantMsgs was dropped in Step 5 (used only by backfillReasoning).
			// We track assistant creation order via a local slice.
			var assistantOrder []string
			for i, step := range tc.steps {
				state.handleStepStart(i)
				if state.currentAssistant != nil {
					assistantOrder = append(assistantOrder, state.currentAssistant.ID)
				}
				for _, r := range step.reasoning {
					state.handleReasoningDelta(r)
				}
				for _, p := range step.prose {
					state.handleDelta(p)
				}
				state.handleStepEnd(i)
			}
			for i, want := range tc.wantReasoning {
				id := assistantOrder[i]
				updated, err := ms.Get(context.Background(), id)
				require.NoError(t, err)
				assert.Equal(t, want, updated.ReasoningContent().Thinking, "step %d reasoning", i)
			}
		})
	}
}

// TestIntegration_RunState_MultiStepTurn exercises the full handler sequence for
// a multi-step turn (step0: reasoning+cmd, step1: reasoning+prose+cmd, step2: reasoning+prose, StopReasonFinal).
// This is the logos v2.0 callback integration path.
func TestIntegration_RunState_MultiStepTurn(t *testing.T) {
	t.Parallel()
	ms := newMockMessageService()
	state := &runState{
		sessionID: "s1",
		logosCfg:  logos.Config{Model: "test-model", Provider: &mockProvider{}},
		messages:  ms,
		ctx:       context.Background(),
	}

	// Step 0: reasoning + cmd (no prose)
	state.handleStepStart(0)
	state.handleReasoningDelta("Thinking about the problem...")
	state.handleDelta("<cmd>echo step0</cmd>")
	state.handleStepEnd(0)
	pendingID0 := state.pendingResult.ID
	state.handleCommandResult("echo step0", "step0 output\n", 0)
	assert.Nil(t, state.pendingResult)

	// Step 1: reasoning + prose + cmd
	state.handleStepStart(1)
	state.handleReasoningDelta("More reasoning...")
	state.handleDelta("Here is my analysis:")
	state.handleDelta(" The data shows X.")
	state.handleDelta("<cmd>process data</cmd>")
	state.handleStepEnd(1)
	pendingID1 := state.pendingResult.ID
	state.handleCommandResult("process data", "processed!\n", 0)
	assert.Nil(t, state.pendingResult)

	// Step 2: reasoning + prose, no cmd → StopReasonFinal
	state.handleStepStart(2)
	state.handleReasoningDelta("Final thoughts...")
	state.handleDelta("Conclusion reached.")
	state.handleStepEnd(2)
	state.handleTurnEnd(logos.StopReasonFinal)
	assert.Nil(t, state.currentAssistant)

	// Verify: 3 assistant messages exist (order is deterministic via m.order).
	var assistants []message.Message
	var results []message.Message
	for _, id := range ms.order {
		msg := ms.messages[id]
		switch msg.Role {
		case message.Assistant:
			assistants = append(assistants, msg)
		case message.Result:
			results = append(results, msg)
		}
	}
	require.Len(t, assistants, 3)
	require.Len(t, results, 2)

	// Step 0: assistant has reasoning + verbatim cmd block.
	assert.Equal(t, "Thinking about the problem...", assistants[0].ReasoningContent().Thinking)
	assert.Contains(t, assistants[0].Content().Text, "<cmd>echo step0</cmd>")
	assert.Equal(t, message.FinishReasonToolUse, assistants[0].FinishReason())

	// Step 1: assistant has reasoning + prose + verbatim cmd block.
	assert.Equal(t, "More reasoning...", assistants[1].ReasoningContent().Thinking)
	assert.Contains(t, assistants[1].Content().Text, "<cmd>process data</cmd>")
	assert.Contains(t, assistants[1].Content().Text, "Here is my analysis:")
	assert.Equal(t, message.FinishReasonToolUse, assistants[1].FinishReason())

	// Step 2: assistant has reasoning + prose, no cmd, final reason.
	assert.Equal(t, "Final thoughts...", assistants[2].ReasoningContent().Thinking)
	assert.Equal(t, "Conclusion reached.", assistants[2].Content().Text)
	assert.Equal(t, message.FinishReasonEndTurn, assistants[2].FinishReason())

	// Verify: 2 result messages with both Command and Output populated.
	for _, resultMsg := range results {
		cmdContent := resultMsg.CommandContent()
		require.NotEqual(t, "", cmdContent.Command, "Command should be populated")
		require.NotEqual(t, "", cmdContent.Output, "Output should be populated")
		assert.False(t, cmdContent.Pending)
	}

	// Verify: pending IDs were resolved.
	pending0Msg, _ := ms.Get(context.Background(), pendingID0)
	assert.Contains(t, pending0Msg.CommandContent().Output, "step0 output")
	pending1Msg, _ := ms.Get(context.Background(), pendingID1)
	assert.Contains(t, pending1Msg.CommandContent().Output, "processed!")

	// Verify: no empty-text assistant (placeholder is fine if reasoning attached).
	for _, asst := range assistants {
		text := asst.Content().Text
		if text == "" {
			// Empty text is OK only if reasoning was attached.
			require.NotEqual(t, "", asst.ReasoningContent().Thinking, "empty-text assistant must have reasoning")
		}
	}
}
