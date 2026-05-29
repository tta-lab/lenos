package agent

import (
	"context"
	"errors"
	"io"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/temenos/client"

	"github.com/tta-lab/lenos/internal/message"
)

// --- Test fakes ---

// scriptedModel returns a sequence of canned emits via Stream(). Each call
// to Stream consumes one entry; missing entries panic the test.
type scriptedModel struct {
	mu       sync.Mutex
	emits    []string
	usages   []fantasy.Usage // optional: per-emit usage override; default Usage{1,1}
	errOn    []int           // call indices (pre-increment) where Stream yields an error
	calls    int
	modelID  string
	provider string
}

// recorderIface is a local substitute for the removed transcript.Recorder.
type recorderIface interface {
	Open(context.Context) error
	UserMessage(context.Context, string, string) error
	AgentEmit(context.Context, string, string) error
	ProseMessage(context.Context, string, string) error
	BashResult(context.Context, string, []byte, int, time.Duration) error
	BashSkipped(context.Context, string, string, string) error
	RuntimeEvent(context.Context, string, string, string) error
	TurnEnd(context.Context, string) error
	Close() error
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
	// Check for error injection
	for _, errIdx := range m.errOn {
		if m.calls == errIdx {
			m.calls++
			seq := iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
				yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: errors.New("scripted error")})
			})
			return seq, nil
		}
	}
	out := m.emits[m.calls]
	u := fantasy.Usage{InputTokens: 1, OutputTokens: 1}
	if m.calls < len(m.usages) {
		u = m.usages[m.calls]
	}
	m.calls++
	seq := iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: out}) {
			return
		}
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, Usage: u}) {
			return
		}
	})
	return seq, nil
}

// streamCapturingModel wraps a fantasy.LanguageModel and records each Stream() call's
// prompt messages so tests can assert on what the model receives (not just what
// is persisted). Other LanguageModel methods (Generate, GenerateObject, StreamObject)
// are delegated to inner without capture. Thread-safe for parallel test use.
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

func (m *scriptedModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	panic("not used")
}

func (m *scriptedModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	panic("not used")
}

var _ fantasy.LanguageModel = (*scriptedModel)(nil)

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
		yield(fantasy.StreamPart{
			Type:  fantasy.StreamPartTypeFinish,
			Usage: fantasy.Usage{InputTokens: 2, OutputTokens: 3},
		})
	}, nil
}

func (m *retryableErrorThenSuccessModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	panic("not used")
}

func (m *retryableErrorThenSuccessModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	panic("not used")
}

var _ fantasy.LanguageModel = (*retryableErrorThenSuccessModel)(nil)

type prefillCapturingModel struct {
	inner        fantasy.LanguageModel
	prefills     []string
	normalCalls  int
	prefillCalls int
	mu           sync.Mutex
}

func (m *prefillCapturingModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	m.normalCalls++
	m.mu.Unlock()
	return m.inner.Stream(ctx, call)
}

func (m *prefillCapturingModel) StreamAssistantPrefill(ctx context.Context, call fantasy.Call, prefill string) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	m.prefillCalls++
	m.prefills = append(m.prefills, prefill)
	m.mu.Unlock()
	return m.inner.Stream(ctx, call)
}

func (m *prefillCapturingModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	return m.inner.Generate(ctx, call)
}

func (m *prefillCapturingModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return m.inner.GenerateObject(ctx, call)
}

func (m *prefillCapturingModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return m.inner.StreamObject(ctx, call)
}

func (m *prefillCapturingModel) Provider() string { return m.inner.Provider() }
func (m *prefillCapturingModel) Model() string    { return m.inner.Model() }

var _ fantasy.LanguageModel = (*prefillCapturingModel)(nil)

// fakeRunner returns canned ExecResults in order. Tests use it to drive
// classify=exec branches without touching /bin/bash.
type fakeRunner struct {
	mu      sync.Mutex
	results []ExecResult
	calls   int
	bash    []string
	onRun   func(bash string, env map[string]string, paths []client.AllowedPath)
}

func (r *fakeRunner) Run(_ context.Context, bash string, env map[string]string, paths []client.AllowedPath) ExecResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bash = append(r.bash, bash)
	if r.onRun != nil {
		r.onRun(bash, env, paths)
	}
	if r.calls >= len(r.results) {
		panic("fakeRunner: ran out of canned results")
	}
	out := r.results[r.calls]
	r.calls++
	return out
}

// --- Helpers ---

func newDeps(t *testing.T, model fantasy.LanguageModel, runner Runner, rec recorderIface) (loopDeps, *mockMessageService) {
	return newDepsWithDrain(t, model, runner, rec, nil)
}

func newDepsWithDrain(t *testing.T, model fantasy.LanguageModel, runner Runner, rec recorderIface, drain func() []string) (loopDeps, *mockMessageService) {
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

func cannedDrainer(rounds ...[]string) func() []string {
	i := 0
	return func() []string {
		if i >= len(rounds) {
			return nil
		}
		out := rounds[i]
		i++
		return out
	}
}

// --- Tests ---

func TestRunLoop_BareExit(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"exit"}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, &fakeRunner{}, rec)

	stop, err := runLoop(context.Background(), deps, nil, "do nothing")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	// One assistant row, finished EndTurn.
	assistants := assistantsByOrder(ms)
	require.Len(t, assistants, 1)
	assert.Equal(t, message.FinishReasonEndTurn, assistants[0].FinishReason())
	assert.Equal(t, "exit", strings.TrimSpace(assistants[0].Content().Text))
}

func TestRunLoop_ExecThenExit(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"echo hi", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: []byte("hi\n"), ExitCode: 0, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "say hi")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	// DB has assistant rows + one result row, runner saw the bash.
	assert.Equal(t, []string{"echo hi"}, runner.bash)
	assert.Len(t, resultsByOrder(ms), 1)
}

func TestRunLoop_RetriesRetryableStreamEOFAndClearsPartialEmit(t *testing.T) {
	t.Parallel()
	model := &retryableErrorThenSuccessModel{
		firstDelta: "partial bad emit",
		secondEmit: "exit",
	}
	deps, ms := newDeps(t, model, &fakeRunner{}, &recordingRecorder{})

	stop, err := runLoop(context.Background(), deps, nil, "do nothing")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Equal(t, 2, model.attempts)

	assistants := assistantsByOrder(ms)
	require.Len(t, assistants, 1)
	assert.Equal(t, "exit", assistants[0].Content().Text)
	assert.NotContains(t, assistants[0].Content().Text, "partial bad emit")
}

func TestRunLoop_EmptyEmitRePrompts(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"   ", "exit"}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, &fakeRunner{}, rec)

	stop, err := runLoop(context.Background(), deps, nil, "noop")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	// Observation persisted as Result row.
	results := messagesByRole(ms, message.Result)
	require.Len(t, results, 1)
	assert.True(t, strings.HasPrefix(results[0].Content().Text, "[runtime] your last response was empty"))

	// Assistant has FinishReasonToolUse for re-prompt branch.
	assistants := assistantsByOrder(ms)
	require.NotEmpty(t, assistants)
	fp := assistants[0].FinishPart()
	require.NotNil(t, fp)
	assert.Equal(t, message.FinishReasonToolUse, fp.Reason)
}

func TestRunLoop_InvalidBashRePrompts(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"if true then", "exit"}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, &fakeRunner{}, rec)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	results := messagesByRole(ms, message.Result)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Content().Text, "[runtime] your last response was not valid bash")

	// Assistant has FinishReasonToolUse for invalid-bash branch.
	assistants := assistantsByOrder(ms)
	require.NotEmpty(t, assistants)
	fp := assistants[0].FinishPart()
	require.NotNil(t, fp)
	assert.Equal(t, message.FinishReasonToolUse, fp.Reason)
}

func TestRunLoop_BannedPatternIsAnnouncedAndSkipped(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{`sed -i 's/a/b/' f.txt`, "exit"}}
	deps, ms := newDeps(t, model, &fakeRunner{}, &recordingRecorder{})

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	// Banned pattern: assistant should be marked with FinishReasonToolUse.
	assistants := assistantsByOrder(ms)
	require.NotEmpty(t, assistants)
	fp := assistants[0].FinishPart()
	require.NotNil(t, fp)
	assert.Equal(t, message.FinishReasonToolUse, fp.Reason)
}

func TestRunLoop_TimeoutRecordsResultAndRePrompts(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"sleep 5", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: []byte("partial"), ExitCode: -1, Err: context.DeadlineExceeded, Duration: 120 * time.Second},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	// Timeout result row has ExitCode=124 and re-prompt text.
	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	assert.NotNil(t, results[0].CommandContent().ExitCode)
	assert.Equal(t, 124, *results[0].CommandContent().ExitCode)
	assert.Contains(t, results[0].CommandContent().Output, "exceeded the per-call timeout")

	// No separate User message for timeout re-prompt.
	users := messagesByRole(ms, message.User)
	assert.Empty(t, users, "timeout must NOT persist a separate User message")
}

func TestRunLoop_StepCapHaltsLoop(t *testing.T) {
	t.Parallel()

	emits := make([]string, StepCap+1)
	for i := range emits {
		emits[i] = "echo " + intString(i)
	}
	results := make([]ExecResult, StepCap+1)
	for i := range results {
		results[i] = ExecResult{Stdout: []byte("ok\n"), ExitCode: 0}
	}
	model := &scriptedModel{emits: emits}
	runner := &fakeRunner{results: results}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrStepCap))
	assert.Equal(t, stopStepCap, stop)

	// Step cap: last assistant message should be EndTurn.
	assistants := assistantsByOrder(ms)
	require.GreaterOrEqual(t, len(assistants), 1)
}

func TestRunLoop_ContextCancelMidExec(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"sleep 5", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{ExitCode: -1, Err: context.Canceled, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stop, err := runLoop(ctx, deps, nil, "")
	assert.Equal(t, stopCanceled, stop)
	assert.True(t, errors.Is(err, context.Canceled), "got %v", err)

	// No orphaned Pending Result row (the loop must abandon it on cancel).
	for _, m := range ms.messages {
		cc := m.CommandContent()
		assert.False(t, cc.Pending, "pending row left behind: %+v", cc)
	}
}

func TestRunLoop_ExecPersistsResultRow(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"go test ./...", "exit"}}
	exitCode := 0
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: []byte("PASS\n"), ExitCode: exitCode, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	_, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)

	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	cc := results[0].CommandContent()
	assert.Equal(t, "go test ./...", cc.Command)
	assert.Equal(t, "PASS\n", cc.Output)
	require.NotNil(t, cc.ExitCode)
	assert.Equal(t, 0, *cc.ExitCode)
	assert.False(t, cc.Pending)
}

func TestRunLoop_TrailingExitDoesNotEndTurn(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{`printf '%s\n' "hi" && exit`, "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: []byte("hi\n"), ExitCode: 0, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "say hi")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	require.Equal(t, []string{`printf '%s\n' "hi" && exit`}, runner.bash)
	assistants := assistantsByOrder(ms)
	require.Len(t, assistants, 2)
	assert.Equal(t, message.FinishReasonToolUse, assistants[0].FinishReason())
	assert.Equal(t, message.FinishReasonEndTurn, assistants[1].FinishReason())
}

func TestRunLoop_TrailingExitFailureDoesNotEndTurn(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{`false && exit`, "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{ExitCode: 1, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	assistants := assistantsByOrder(ms)
	require.Len(t, assistants, 2)
	assert.Equal(t, message.FinishReasonToolUse, assistants[0].FinishReason())
	assert.Equal(t, message.FinishReasonEndTurn, assistants[1].FinishReason())
}

func TestRunLoop_TrailingExit_NaturalLanguageRePromptsWithoutRunning(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{`Let me start && exit`, "exit"}}
	runner := &fakeRunner{}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	assert.Empty(t, runner.bash)
	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	obs := results[0].Content().Text
	assert.Contains(t, obs, `m"`)
	assert.NotContains(t, obs, "narrate")
}

// --- Mock helpers ---

func messagesByRole(ms *mockMessageService, role message.MessageRole) []message.Message {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	var out []message.Message
	for _, id := range ms.order {
		m := ms.messages[id]
		if m.Role == role {
			out = append(out, m)
		}
	}
	return out
}

func assistantsByOrder(ms *mockMessageService) []message.Message {
	return messagesByRole(ms, message.Assistant)
}

func resultsByOrder(ms *mockMessageService) []message.Message {
	return messagesByRole(ms, message.Result)
}

// TestRunLoop_NonCancelStreamError verifies that a provider-level stream error
// (non-cancel, e.g. 500 / rate-limit) maps to stopError and propagates the
// original error without treating it as cancellation.
func TestRunLoop_NonCancelStreamError(t *testing.T) {
	t.Parallel()

	providerErr := errors.New("upstream error: 503 Service Unavailable")
	model := &errorStreamModel{err: providerErr}
	rec := new(recordingRecorder)

	deps, _ := newDeps(t, model, &fakeRunner{}, rec)
	_, runErr := runLoop(t.Context(), deps, nil, "prompt")

	require.Error(t, runErr, "runLoop should return an error for non-cancel stream error")
	require.True(t, errors.Is(runErr, providerErr),
		"original error should be preserved (not wrapped as cancel)")

	// stopError is returned, not stopCanceled — confirm via error type.
	require.NotEqual(t, context.Canceled, runErr,
		"non-cancel error must not be coerced to Canceled")
}

// errorStreamModel always returns a fixed non-cancel error from Stream.
type errorStreamModel struct {
	err error
}

func (m *errorStreamModel) Model() string    { return "error-model" }
func (m *errorStreamModel) Provider() string { return "test" }
func (m *errorStreamModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	panic("not used")
}

func (m *errorStreamModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	return nil, m.err
}

func (m *errorStreamModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	panic("not used")
}

func TestRunLoop_OriginalPromptFiresUserMessage(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"exit"}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, &fakeRunner{}, rec)

	stop, err := runLoop(context.Background(), deps, nil, "hello world")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	// Exit assistant should be EndTurn.
	assistants := assistantsByOrder(ms)
	require.NotEmpty(t, assistants)
	fp := assistants[len(assistants)-1].FinishPart()
	require.NotNil(t, fp)
	assert.Equal(t, message.FinishReasonEndTurn, fp.Reason)
}

func TestRunLoop_DrainQueueEmpty_NoOp(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"echo hi", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: []byte("hi\n"), ExitCode: 0, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDepsWithDrain(t, model, runner, rec, cannedDrainer())

	stop, err := runLoop(context.Background(), deps, nil, "test prompt")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	// Exit assistant should be EndTurn.
	assert.Empty(t, messagesByRole(ms, message.User))
}

func TestRunLoop_DrainOneOnExec(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"echo hi", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: []byte("hi\n"), ExitCode: 0, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDepsWithDrain(t, model, runner, rec, cannedDrainer([]string{"follow up"}))

	stop, err := runLoop(context.Background(), deps, nil, "original")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1)
	assert.Equal(t, "follow up", users[0].Content().Text)
}

func TestRunLoop_DrainManyPreservesOrder(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"echo hi", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: []byte("hi\n"), ExitCode: 0, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDepsWithDrain(t, model, runner, rec, cannedDrainer([]string{"m1", "m2", "m3"}))

	stop, err := runLoop(context.Background(), deps, nil, "original")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 3)
	assert.Equal(t, "m1", users[0].Content().Text)
	assert.Equal(t, "m2", users[1].Content().Text)
	assert.Equal(t, "m3", users[2].Content().Text)
}

func TestRunLoop_PostStepHookCalledPerStep(t *testing.T) {
	t.Parallel()
	var (
		mu     sync.Mutex
		steps  []int
		usages []fantasy.Usage
	)
	model := &scriptedModel{emits: []string{"echo one", "echo two", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: []byte("one\n"), ExitCode: 0, Duration: time.Millisecond},
		{Stdout: []byte("two\n"), ExitCode: 0, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, _ := newDeps(t, model, runner, rec)
	deps.postStepHook = func(stepIdx int, u fantasy.Usage) {
		mu.Lock()
		defer mu.Unlock()
		steps = append(steps, stepIdx)
		usages = append(usages, u)
	}

	stop, err := runLoop(context.Background(), deps, nil, "start")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, steps, 3, "postStepHook should be called 3 times (2 exec + 1 exit)")
	assert.Equal(t, 0, steps[0])
	assert.Equal(t, 1, steps[1])
	assert.Equal(t, 2, steps[2])
	// Each step should have non-zero usage
	for i, u := range usages {
		if u.InputTokens == 0 && u.OutputTokens == 0 && u.TotalTokens == 0 {
			t.Fatalf("step %d: expected non-zero usage", i)
		}
	}
}

func TestRunLoop_PostStepHookFiresBeforeOnUsage(t *testing.T) {
	t.Parallel()
	var hookSteps []int
	var usageSteps []int
	model := &scriptedModel{emits: []string{"fail", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: []byte("out\n"), ExitCode: 1, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, _ := newDeps(t, model, runner, rec)
	deps.postStepHook = func(stepIdx int, u fantasy.Usage) {
		hookSteps = append(hookSteps, stepIdx)
	}
	deps.onUsage = func(stepIdx int, u fantasy.Usage, m fantasy.ProviderMetadata) {
		usageSteps = append(usageSteps, stepIdx)
	}

	stop, err := runLoop(context.Background(), deps, nil, "go")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	// postStepHook fires for every step, onUsage fires for every step
	require.Len(t, hookSteps, 2, "postStepHook: both steps")
	require.Len(t, usageSteps, 2, "onUsage: both steps")
}

func TestRunLoop_PostStepHookExecutesBeforePreStepAutoCompact(t *testing.T) {
	t.Parallel()
	var hookCalled bool
	var usageCalled bool
	model := &scriptedModel{emits: []string{"echo ok"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: []byte("ok\n"), ExitCode: 0, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, _ := newDeps(t, model, runner, rec)
	deps.postStepHook = func(int, fantasy.Usage) {
		hookCalled = true
	}
	deps.onUsage = func(int, fantasy.Usage, fantasy.ProviderMetadata) {
		usageCalled = true
	}
	deps.shouldSummarizeBeforeStep = func(stepIdx int) bool {
		return stepIdx > 0
	}

	stop, err := runLoop(context.Background(), deps, nil, "go")
	require.NoError(t, err)
	assert.Equal(t, stopShouldSummarize, stop)
	if !hookCalled {
		t.Fatal("postStepHook was not called before pre-step compact")
	}
	if !usageCalled {
		t.Fatal("onUsage was not called before pre-step compact")
	}
}

func TestRunLoop_PostStepHookNil(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"exit"}}
	rec := &recordingRecorder{}
	deps, _ := newDeps(t, model, &fakeRunner{}, rec)
	// postStepHook is nil by default — should not panic

	stop, err := runLoop(context.Background(), deps, nil, "go")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
}

func TestRunLoop_DrainOnEmptyEmit(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"   ", "exit"}}
	rec := &recordingRecorder{}
	deps, ms := newDepsWithDrain(t, model, &fakeRunner{}, rec, cannedDrainer([]string{"q1"}))

	stop, err := runLoop(context.Background(), deps, nil, "noop")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	// Re-prompt is a Result, drained prompt is a User message.
	results := messagesByRole(ms, message.Result)
	require.Len(t, results, 1)
	assert.True(t, strings.HasPrefix(results[0].Content().Text, "[runtime]"))
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1)
	assert.Equal(t, "q1", users[0].Content().Text)
}

func TestRunLoop_DrainOnBanned(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{`sed -i "s/a/b/" f.txt`, "exit"}}
	rec := &recordingRecorder{}
	deps, ms := newDepsWithDrain(t, model, &fakeRunner{}, rec, cannedDrainer([]string{"q1"}))

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	// Re-prompt is a Result, drained prompt is a User message.
	results := messagesByRole(ms, message.Result)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Content().Text, "[runtime]")
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1)
	assert.Equal(t, "q1", users[0].Content().Text)
}

func TestRunLoop_DrainOnTimeout(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"sleep 5", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Err: context.DeadlineExceeded, Duration: time.Second * 120},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDepsWithDrain(t, model, runner, rec, cannedDrainer([]string{"q1"}))

	stop, err := runLoop(context.Background(), deps, nil, "run slow")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	// Only the drained prompt is a User message — re-prompt is in the result row.
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1)
	assert.Equal(t, "q1", users[0].Content().Text)

	// Timeout result row has ExitCode=124 and re-prompt text.
	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	assert.NotNil(t, results[0].CommandContent().ExitCode)
	assert.Equal(t, 124, *results[0].CommandContent().ExitCode)
	assert.Contains(t, results[0].CommandContent().Output, "exceeded the per-call timeout")
}

func TestRunLoop_BackgroundJobStartMentionsKillWithoutStatusPolling(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"sleep 20", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Background: true, JobID: "job-123", Duration: time.Second * 16},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "run slow")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	obs := results[0].CommandContent().Observation
	assert.Contains(t, obs, "temenos job kill job-123")
	assert.NotContains(t, obs, "check status")
	assert.NotContains(t, obs, "temenos job list")
	assert.NotContains(t, obs, "temenos job log")
}

func TestRunLoop_DrainOnCmdNotFound(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"nopebinary", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stderr: []byte("bash: nopebinary: command not found\n"), ExitCode: 127, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDepsWithDrain(t, model, runner, rec, cannedDrainer([]string{"q1"}))

	stop, err := runLoop(context.Background(), deps, nil, "run nope")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	// Re-prompt is in the result row, only the drained prompt is a User message.
	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	assert.NotNil(t, results[0].CommandContent().ExitCode)
	assert.Equal(t, 1, *results[0].CommandContent().ExitCode)
	assert.Contains(t, results[0].CommandContent().Output, alertPrefix)

	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1)
	assert.Equal(t, "q1", users[0].Content().Text)
}

func (m *errorStreamModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	panic("not used")
}

var _ fantasy.LanguageModel = (*errorStreamModel)(nil)

func TestRunLoop_PreStepCompactReturnsShouldSummarize(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"echo a", "echo b"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: []byte("a\n"), ExitCode: 0, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)
	var calls int
	deps.onUsage = func(_ int, _ fantasy.Usage, _ fantasy.ProviderMetadata) {
		calls++
	}
	deps.shouldSummarizeBeforeStep = func(stepIdx int) bool {
		return stepIdx > 0 && calls >= 1
	}

	stop, err := runLoop(context.Background(), deps, nil, "do work")
	require.NoError(t, err)
	assert.Equal(t, stopShouldSummarize, stop)
	assert.Equal(t, 1, calls, "onUsage should fire for the completed emit before compact")

	assistants := assistantsByOrder(ms)
	require.NotEmpty(t, assistants)
	assert.Equal(t, message.FinishReasonToolUse, assistants[len(assistants)-1].FinishReason())

	results := resultsByOrder(ms)
	require.Len(t, results, 1, "pre-step compact should happen after the completed bash result")
}

// TestRunLoop_CmdNotFound_PassesStderrToken verifies that when bash prints
// "command not found" in stderr the loop invokes rePromptCmdNotFound with
// the token captured from stderr. The re-prompt must contain the word.
func TestRunLoop_CmdNotFound_PassesStderrToken(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"lorem ipsum", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stderr: []byte("bash: lorem: command not found\n"), ExitCode: 127, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "do it")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	// Re-prompt observation is in the result row, not a separate User message.
	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	assert.NotNil(t, results[0].CommandContent().ExitCode)
	assert.Equal(t, 1, *results[0].CommandContent().ExitCode)
	assert.Contains(t, results[0].CommandContent().Output, "`lorem`", "re-prompt must reference stderr token")
	assert.Contains(t, results[0].CommandContent().Output, "command not found")

	// No additional User message created for cmd-not-found re-prompt.
	users := messagesByRole(ms, message.User)
	assert.Empty(t, users, "cmd-not-found must NOT persist a separate User message")
}

// TestRunLoop_CmdNotFound_EmptyStderr_NoRePrompt covers the case where the
// runner returns exit 127 but stderr has no "command not found" pattern.
// The re-prompt must NOT fire — the exit code alone is not sufficient.
func TestRunLoop_CmdNotFound_EmptyStderr_NoRePrompt(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"somecommand", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{ExitCode: 127, Stderr: nil, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	users := messagesByRole(ms, message.User)
	require.Len(t, users, 0, "exit 127 with no command-not-found in stderr must NOT fire re-prompt")
}

// TestRunLoop_Exit127_RePromptPersisted ensures the re-prompt observation is
// persisted as a User-role DB row so future history builds include guidance.
func TestRunLoop_Exit127_RePromptPersisted(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"unknowncmd --flag", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stderr: []byte("bash: unknowncmd: command not found\n"), ExitCode: 127, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	// Re-prompt observation is in the result row, not a separate User message.
	results := resultsByOrder(ms)
	require.Len(t, results, 1, "exactly one result row must be persisted")
	assert.NotNil(t, results[0].CommandContent().ExitCode)
	assert.Equal(t, 1, *results[0].CommandContent().ExitCode)
	assert.Contains(t, results[0].CommandContent().Output, alertPrefix)
	assert.Contains(t, results[0].CommandContent().Output, "`unknowncmd`")

	// No additional User message created for cmd-not-found.
	users := messagesByRole(ms, message.User)
	assert.Empty(t, users, "cmd-not-found must NOT persist a separate User message")
}

// TestRunLoop_Exit127_ThenExit covers the case where the model emits a
// command+exit sequence, bash prints "command not found", and then the model
// correctly exits on the next turn.
func TestRunLoop_Exit127_ThenExit(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"nope --bad", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stderr: []byte("bash: nope: command not found\n"), ExitCode: 127, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "run unknown cmd")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	// Two assistant rows: the initial emit and the exit.
	assistants := assistantsByOrder(ms)
	require.Len(t, assistants, 2)
	assert.Equal(t, "nope --bad", strings.TrimSpace(assistants[0].Content().Text))

	// Re-prompt is in the result row, not a separate User message.
	results := resultsByOrder(ms)
	require.Len(t, results, 1, "result row must exist for command-not-found")
	assert.NotNil(t, results[0].CommandContent().ExitCode)
	assert.Equal(t, 1, *results[0].CommandContent().ExitCode)
	assert.Contains(t, results[0].CommandContent().Output, "command not found")

	// No separate User message.
	users := messagesByRole(ms, message.User)
	assert.Empty(t, users, "cmd-not-found must NOT persist a separate User message")
}

// TestRunLoop_Exit127_LowercaseProseRePrompts tests that lowercase prose,
// which is outside the natural-language auto-md heuristic, still gets
// cmd-not-found guidance when bash reports it as a missing command.
func TestRunLoop_Exit127_LowercaseProseRePrompts(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"hello world", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stderr: []byte("bash: hello: command not found\n"), ExitCode: 127, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "find")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	results := messagesByRole(ms, message.Result)
	require.Len(t, results, 1)
	obs := results[0].CommandContent().Output
	assert.Contains(t, obs, "`hello`")
	assert.Contains(t, obs, `m"`)
	assert.NotContains(t, obs, "narrate")
}

// TestRunLoop_CmdNotFound_RePromptIncludesFenceGuidance tests that the
// rePromptCmdNotFound template names markdown fences without echoing a
// copyable fence token back to the model.
func TestRunLoop_CmdNotFound_RePromptIncludesFenceGuidance(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"notarealcmd", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stderr: []byte("bash: notarealcmd: command not found\n"), ExitCode: 127, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "run")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	assert.NotNil(t, results[0].CommandContent().ExitCode)
	assert.Equal(t, 1, *results[0].CommandContent().ExitCode)
	obs := results[0].CommandContent().Output
	assert.Contains(t, obs, "markdown fence")
	assert.NotContains(t, obs, "```")

	users := messagesByRole(ms, message.User)
	assert.Empty(t, users, "cmd-not-found must NOT persist a separate User message")
}

// TestRunLoop_Exit127_NonExitNotAffected confirms that a non-127 exit code
// does NOT trigger the rePromptCmdNotFound path — it uses the standard
// formatResultForModel envelope instead.
func TestRunLoop_Exit127_NonExitNotAffected(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"ls /nonexistent", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: nil, Stderr: []byte("ls: /nonexistent: No such file or directory\n"), ExitCode: 1, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	// Non-127 exit code: no User-role re-prompt is persisted.
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 0, "non-127 exit must NOT persist a User-role re-prompt")
}

// TestRunLoop_ProseThenCommand_StderrMatch_FiresRePrompt covers the stderr-scan
// failure mode from session 1bd0d74e: a multi-line emit whose first token is
// lowercase (so natural-language coercion doesn't fire), but bash reports
// "command not found" on an internal line and the trailing real command exits 0,
// making the overall exit 0. The old exit-127 gate missed these; stderr-scan catches them.
//
// Note: natural-language emits like "The PR already exists..." now receive a
// message-block repair prompt and never reach the stderr-scan path.
func TestRunLoop_ProseThenCommand_StderrMatch_FiresRePrompt(t *testing.T) {
	t.Parallel()
	// Emit starts with lowercase (bypasses natural-language coercion) but contains a
	// not-found token internally. bash runs, reports "command not found" for
	// "nonexistentcmd" in stderr while the trailing real command succeeds (exit 0).
	inner := &scriptedModel{emits: []string{
		"nonexistentcmd --flag\ntask abc123 done",
		"exit",
	}}
	cm := &streamCapturingModel{inner: inner}
	runner := &fakeRunner{results: []ExecResult{
		{
			Stdout:   []byte("Completed task abc123.\n"),
			Stderr:   []byte("bash: line 1: nonexistentcmd: command not found\n"),
			ExitCode: 0,
			Duration: time.Millisecond,
		},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, cm, runner, rec)

	_, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)

	// Re-prompt is in the result row, not a separate User message.
	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	assert.NotNil(t, results[0].CommandContent().ExitCode)
	assert.Equal(t, 1, *results[0].CommandContent().ExitCode)
	assert.Contains(t, results[0].CommandContent().Output, "`nonexistentcmd`", "re-prompt must capture the first not-found token from stderr")

	// No separate User message created for cmd-not-found re-prompt.
	users := messagesByRole(ms, message.User)
	assert.Empty(t, users, "cmd-not-found must NOT persist a separate User message")

	// Salience flip: the second Stream() call must receive the alert BEFORE the
	// result envelope. cm.captured[1] is the prompt for the re-prompt turn.
	require.Len(t, cm.captured, 2, "expected exactly two Stream() calls")
	lastUserMsg := cm.captured[1][len(cm.captured[1])-1]
	var obs string
	for _, part := range lastUserMsg.Content {
		if tp, ok := part.(fantasy.TextPart); ok {
			obs += tp.Text
		}
	}
	alertIdx := strings.Index(obs, alertPrefix)
	envelopeIdx := strings.Index(obs, "<result>")
	require.GreaterOrEqual(t, alertIdx, 0, "alert prefix must be present")
	require.GreaterOrEqual(t, envelopeIdx, 0, "result envelope must be present")
	assert.Less(t, alertIdx, envelopeIdx, "alert MUST appear before envelope (salience flip)")
}

// TestRunLoop_GrepNoMatch_NoRePrompt confirms that a legit exit-1 (grep with
// no match) does NOT trigger the re-prompt — stderr has no command-not-found.
func TestRunLoop_GrepNoMatch_NoRePrompt(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"grep needle haystack", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: nil, Stderr: nil, ExitCode: 1, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	_, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)

	for _, u := range messagesByRole(ms, message.User) {
		assert.NotContains(t, u.Content().Text, "[runtime]",
			"exit 1 with no command-not-found pattern in stderr must NOT fire re-prompt")
	}
}

// TestRunLoop_ProseThenCommand_ModelSeesEnvelopeAndRePrompt asserts that the
// model's Stream() receives both the full <result> envelope and the [ALERT from runtime]
// re-prompt when stderr contains "command not found". Guards against a regression
// where the result envelope is silently dropped from the model's context.
//
// Note: natural-language emits like "The PR already exists..." now receive a
// message-block repair prompt; this test uses a lowercase-starting emit so it exercises the
// post-exec stderr-scan path.
func TestRunLoop_ProseThenCommand_ModelSeesEnvelopeAndRePrompt(t *testing.T) {
	t.Parallel()
	inner := &scriptedModel{emits: []string{
		"nonexistentcmd --flag\ntask abc123 done",
		"exit",
	}}
	cm := &streamCapturingModel{inner: inner}
	runner := &fakeRunner{results: []ExecResult{
		{
			Stdout:   []byte("Completed task abc123.\n"),
			Stderr:   []byte("bash: line 1: nonexistentcmd: command not found\n"),
			ExitCode: 0,
			Duration: time.Millisecond,
		},
	}}
	rec := &recordingRecorder{}
	deps, _ := newDeps(t, cm, runner, rec)

	_, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)

	// Second Stream() call carries the re-prompt messages (first is initial prompt).
	require.GreaterOrEqual(t, len(cm.captured), 2,
		"must have at least 2 Stream() calls (initial + re-prompt turn)")

	prompt := cm.captured[1]
	var rePrompt string
	for _, m := range prompt {
		if m.Role != fantasy.MessageRoleUser {
			continue
		}
		if len(m.Content) == 0 {
			continue
		}
		tp, ok := m.Content[0].(fantasy.TextPart)
		if !ok {
			t.Logf("Content[0] type: %T, value: %#v", m.Content[0], m.Content[0])
			continue
		}
		rePrompt = tp.Text
	}
	require.NotEmpty(t, rePrompt, "second Stream() must contain a non-empty User-role message (the re-prompt)")

	// Salience flip: alert FIRST, then <result> envelope.
	alertIdx := strings.Index(rePrompt, alertPrefix)
	envelopeIdx := strings.Index(rePrompt, "<result>")
	require.GreaterOrEqual(t, alertIdx, 0, "model must see the alert prefix")
	require.GreaterOrEqual(t, envelopeIdx, 0, "model must see the result envelope")
	assert.Less(t, alertIdx, envelopeIdx, "alert MUST appear before envelope (salience flip)")
}

// TestRunLoop_CmdNotFound_BashLineNumberFormat verifies the regex captures the
// offending token when bash uses its multi-line script error format
// "bash: line N: <token>: command not found". This format is dominant for
// fence-shape emits (```bash\ncmd\n```) and any multi-line emit in general.
// Without this, fence-shape emits silently fail — no re-prompt fires (validated
// in worker session 7f71f563).
func TestRunLoop_CmdNotFound_BashLineNumberFormat(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"echo before\n652a5f45", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stderr: []byte("bash: line 2: 652a5f45: command not found\n"), ExitCode: 127, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	_, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)

	// Re-prompt observation is in the result row, not a separate User message.
	results := resultsByOrder(ms)
	require.Len(t, results, 1, "re-prompt MUST fire on multi-line bash error format")
	assert.NotNil(t, results[0].CommandContent().ExitCode)
	assert.Equal(t, 1, *results[0].CommandContent().ExitCode)
	obs := results[0].CommandContent().Output
	assert.Contains(t, obs, alertPrefix)
	assert.Contains(t, obs, "`652a5f45`",
		"must capture the offending token even when stderr has 'line N:' prefix")

	// No separate User message.
	users := messagesByRole(ms, message.User)
	assert.Empty(t, users, "cmd-not-found must NOT persist a separate User message")
}

// TestScanFirstCmdNotFound_BothBashErrorFormats locks the regex contract for both
// single-line and multi-line bash error formats. Failing this test means the
// runtime won't re-prompt on a known shape failure mode — high-impact regression.
func TestScanFirstCmdNotFound_BothBashErrorFormats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		stderr string
		want   string
	}{
		{"single-line", "bash: Let: command not found\n", "Let"},
		{"multi-line with line number", "bash: line 2: 652a5f45: command not found\n", "652a5f45"},
		{"multi-line bracket token", "bash: line 4: [216c6f17]: command not found\n", "[216c6f17]"},
		{"first match wins (left-to-right)", "bash: line 2: foo: command not found\nbash: line 4: bar: command not found\n", "foo"},
		{"no match for unrelated stderr", "ls: cannot access '/missing': No such file or directory\n", ""},
		{"no match for partial pattern", "command not found\n", ""},
		{"no match for stdout-style mention", "the binary 'foo' was reported as command not found by the linker\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := scanFirstCmdNotFound(tc.stderr)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestRunLoop_NaturalLanguage_LowercaseAccepted verifies a lowercase first word
// (legit bash command) does NOT trigger natural-language coercion.
func TestRunLoop_NaturalLanguage_LowercaseAccepted(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"ls -la", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: []byte("file1\nfile2\n"), ExitCode: 0, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, _ := newDeps(t, model, runner, rec)

	_, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)

	// Exec ran — lowercase first word is fine.
	assert.Equal(t, []string{"ls -la"}, runner.bash, "lowercase first word must reach exec")
}

// TestRunLoop_ToolCall_FiresPreExec verifies tool-call shaped emits are
// rejected before bash exec and persist a high-salience re-prompt.
func TestRunLoop_ToolCall_FiresPreExec(t *testing.T) {
	t.Parallel()
	inner := &scriptedModel{emits: []string{"<tool_call>{\"name\":\"bash\"}</tool_call>", "exit"}}
	model := &streamCapturingModel{inner: inner}
	runner := &fakeRunner{} // runner must NOT be called
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	_, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)

	assert.Empty(t, runner.bash, "tool-call branch must not invoke runner")

	results := messagesByRole(ms, message.Result)
	require.Len(t, results, 1)
	obs := results[0].Content().Text

	assistants := messagesByRole(ms, message.Assistant)
	require.Len(t, assistants, 1, "bad tool-call assistant emit must be dropped from persistence")
	assert.Equal(t, "exit", strings.TrimSpace(assistants[0].Content().Text))
	assert.Contains(t, obs, alertPrefix)
	assert.Contains(t, obs, "There is NO tool/function calling API")
	assert.Contains(t, obs, "emit plain bash")

	require.Len(t, model.captured, 2, "expected initial prompt and one re-prompt turn")
	prompt := model.captured[1]
	require.NotEmpty(t, prompt)
	last := prompt[len(prompt)-1]
	require.Equal(t, fantasy.MessageRoleUser, last.Role)
	for _, m := range prompt {
		if m.Role != fantasy.MessageRoleAssistant {
			continue
		}
		for _, part := range m.Content {
			if tp, ok := part.(fantasy.TextPart); ok {
				assert.NotContains(t, tp.Text, "<tool_call>", "bad tool-call emit must not be replayed to model")
			}
		}
	}
}

func TestRunLoop_NaturalLanguageAutoWrapsMessageBlock(t *testing.T) {
	t.Parallel()
	emit := "Done. Tests pass."
	model := &scriptedModel{emits: []string{emit, "exit"}}
	runner := &fakeRunner{}
	deps, ms := newDeps(t, model, runner, nil)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Empty(t, runner.bash)

	assistants := assistantsByOrder(ms)
	require.Len(t, assistants, 1)
	assert.Equal(t, `m#"Done. Tests pass."#`, assistants[0].Content().Text)

	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	assert.Equal(t, emit, results[0].CommandContent().Narration)
}

func TestRunLoop_MessageBlockOnlyPublishesAndStops(t *testing.T) {
	t.Parallel()
	emit := "m\"Done for the user.\"\n"
	model := &scriptedModel{emits: []string{emit, "exit"}}
	runner := &fakeRunner{}
	deps, ms := newDeps(t, model, runner, nil)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Equal(t, 1, model.calls)
	assert.Empty(t, runner.bash)

	assistants := assistantsByOrder(ms)
	require.Len(t, assistants, 1)
	assert.Equal(t, emit, assistants[0].Content().Text)

	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	assert.Equal(t, "Done for the user.", results[0].CommandContent().Narration)
}

func TestRunLoop_AssistantPrefillUsesNativeModelSupport(t *testing.T) {
	t.Parallel()
	model := &prefillCapturingModel{inner: &scriptedModel{emits: []string{"\"Done.\"\n"}}}
	runner := &fakeRunner{}
	deps, _ := newDeps(t, model, runner, nil)
	deps.messageBlockPrefill = true

	stop, err := runLoop(context.Background(), deps, nil, "prompt")

	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Equal(t, 1, model.prefillCalls)
	assert.Equal(t, 0, model.normalCalls)
	assert.Equal(t, []string{"m"}, model.prefills)
	assert.Empty(t, runner.bash)
}

func TestRunLoop_AssistantPrefillIgnoredWithoutNativeModelSupport(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"exit"}}
	runner := &fakeRunner{}
	deps, _ := newDeps(t, model, runner, nil)
	deps.messageBlockPrefill = true

	stop, err := runLoop(context.Background(), deps, nil, "prompt")

	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Equal(t, 1, model.calls)
}

func TestRunLoop_AssistantPrefillDisabledUsesNormalStream(t *testing.T) {
	t.Parallel()
	model := &prefillCapturingModel{inner: &scriptedModel{emits: []string{"echo ok", "exit"}}}
	runner := &fakeRunner{results: []ExecResult{{ExitCode: 0}}}
	deps, _ := newDeps(t, model, runner, nil)

	stop, err := runLoop(context.Background(), deps, nil, "prompt")

	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Equal(t, 0, model.prefillCalls)
	assert.Equal(t, 2, model.normalCalls)
	assert.Equal(t, []string{"echo ok"}, runner.bash)
}

func TestRunLoop_MessageBlockOnlyMultipleBlocksPreserveOrder(t *testing.T) {
	t.Parallel()
	emit := "m\"First.\"\nm\"Second.\"\n"
	model := &scriptedModel{emits: []string{emit, "exit"}}
	runner := &fakeRunner{}
	deps, ms := newDeps(t, model, runner, nil)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Empty(t, runner.bash)

	assistants := assistantsByOrder(ms)
	require.Len(t, assistants, 1)
	assert.Equal(t, emit, assistants[0].Content().Text)

	results := resultsByOrder(ms)
	require.Len(t, results, 2)
	assert.Equal(t, "First.", results[0].CommandContent().Narration)
	assert.Equal(t, "Second.", results[1].CommandContent().Narration)
}

func TestRunLoop_AddressedMessageBlockDeliveryFailureStillStops(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"m(owner)\"Please review.\"\n", "exit"}}
	runner := &fakeRunner{results: []ExecResult{{Stderr: []byte("send failed\n"), ExitCode: 9}}}
	deps, ms := newDeps(t, model, runner, nil)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Equal(t, 1, model.calls)
	require.Len(t, runner.bash, 1)
	assert.Contains(t, runner.bash[0], "ttal send --to 'owner'")

	results := resultsByOrder(ms)
	require.Len(t, results, 2)
	cc := results[0].CommandContent()
	require.NotNil(t, cc.ExitCode)
	assert.Equal(t, 9, *cc.ExitCode)
	assert.Contains(t, cc.Output, "send failed")
	assert.Contains(t, cc.Observation, "message delivery failed")
	assert.Equal(t, "Please review.", results[1].CommandContent().Narration)

	assistants := assistantsByOrder(ms)
	require.Len(t, assistants, 1)
	assert.Equal(t, "m(owner)\"Please review.\"\n", assistants[0].Content().Text)
}

func TestRunLoop_PairWithDeliversUntargetedMessageBlock(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"m\"Please review.\"\n", "exit"}}
	runner := &fakeRunner{results: []ExecResult{{ExitCode: 0}}}
	deps, ms := newDeps(t, model, runner, nil)
	deps.pairWith = "reviewer"

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Equal(t, 1, model.calls)
	require.Len(t, runner.bash, 1)
	assert.Contains(t, runner.bash[0], "ttal send --to 'reviewer'")

	assistants := assistantsByOrder(ms)
	require.Len(t, assistants, 1)
	assert.Equal(t, "m\"Please review.\"\n", assistants[0].Content().Text)
}

func TestRunLoop_MessageBlockTargetOverridesPairWith(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"m(owner)\"Please review.\"\n", "exit"}}
	runner := &fakeRunner{results: []ExecResult{{ExitCode: 0}}}
	deps, _ := newDeps(t, model, runner, nil)
	deps.pairWith = "reviewer"

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	require.Len(t, runner.bash, 1)
	assert.Contains(t, runner.bash[0], "ttal send --to 'owner'")
	assert.NotContains(t, runner.bash[0], "reviewer")
}

func TestRunLoop_MixedSingleLineMessageBlockRunsCleanBashAndStoresNarration(t *testing.T) {
	t.Parallel()
	emit := "m\"Testing now.\"\necho ok\n"
	model := &scriptedModel{emits: []string{emit, "exit"}}
	runner := &fakeRunner{results: []ExecResult{{Stdout: []byte("ok\n"), ExitCode: 0}}}
	deps, ms := newDeps(t, model, runner, nil)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Equal(t, []string{"echo ok\n"}, runner.bash)

	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	cc := results[0].CommandContent()
	assert.Equal(t, "echo ok\n", cc.Command)
	assert.Equal(t, "Testing now.", cc.Narration)

	assistants := assistantsByOrder(ms)
	require.Len(t, assistants, 2)
	assert.Equal(t, emit, assistants[0].Content().Text)
}

func TestRunLoop_MixedMultilineMessageBlockRendersBodyOnSuccess(t *testing.T) {
	t.Parallel()
	emit := "m\"First line.\nSecond line.\"\necho ok\n"
	model := &scriptedModel{emits: []string{emit, "exit"}}
	runner := &fakeRunner{results: []ExecResult{{Stdout: []byte("ok\n"), ExitCode: 0}}}
	deps, ms := newDeps(t, model, runner, nil)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Equal(t, []string{"echo ok\n"}, runner.bash)

	assistants := assistantsByOrder(ms)
	require.Len(t, assistants, 2)
	assert.Equal(t, emit, assistants[0].Content().Text)
	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	assert.Equal(t, "First line.\nSecond line.", results[0].CommandContent().Narration)
}

func TestRunLoop_MixedSingleLineMessageBlockStillDeliversPairWith(t *testing.T) {
	t.Parallel()
	emit := "m\"Please review.\"\necho ok\n"
	model := &scriptedModel{emits: []string{emit, "exit"}}
	runner := &fakeRunner{results: []ExecResult{{Stdout: []byte("ok\n"), ExitCode: 0}, {ExitCode: 0}}}
	deps, ms := newDeps(t, model, runner, nil)
	deps.pairWith = "reviewer"

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	require.Len(t, runner.bash, 2)
	assert.Equal(t, "echo ok\n", runner.bash[0])
	assert.Contains(t, runner.bash[1], "ttal send --to 'reviewer'")

	assistants := assistantsByOrder(ms)
	require.Len(t, assistants, 2)
	assert.Equal(t, emit, assistants[0].Content().Text)
	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	assert.Equal(t, "Please review.", results[0].CommandContent().Narration)
}

func TestRunLoop_MixedMessageBlockSuppressesMessagesOnBashFailure(t *testing.T) {
	t.Parallel()
	emit := "m\"This should not publish.\"\nfalse\n"
	model := &scriptedModel{emits: []string{emit, "exit"}}
	runner := &fakeRunner{results: []ExecResult{{ExitCode: 1}}}
	deps, ms := newDeps(t, model, runner, nil)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Equal(t, []string{"false\n"}, runner.bash)

	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	cc := results[0].CommandContent()
	assert.Equal(t, "false\n", cc.Command)
	assistants := assistantsByOrder(ms)
	require.NotEmpty(t, assistants)
	assert.Equal(t, emit, assistants[0].Content().Text)
	for _, assistant := range assistants[1:] {
		assert.NotEqual(t, "This should not publish.", assistant.Content().Text)
	}
}

func TestRunLoop_MessageBlockSameLineRePromptsWithoutRunningBash(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		emit string
	}{
		{name: "semicolon", emit: "echo ok; m\"Done.\"\n"},
		{name: "background", emit: "sleep 1 & m\"Done.\"\n"},
		{name: "pipeline", emit: "printf ok | m\"Done.\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			model := &scriptedModel{emits: []string{tc.emit, "exit"}}
			runner := &fakeRunner{}
			deps, ms := newDeps(t, model, runner, nil)

			stop, err := runLoop(context.Background(), deps, nil, "")
			require.NoError(t, err)
			assert.Equal(t, stopExit, stop)
			assert.Empty(t, runner.bash)

			results := resultsByOrder(ms)
			require.Len(t, results, 1)
			obs := results[0].Content().Text
			assert.Contains(t, obs, "invalid Lenos Bash")
			assert.Contains(t, obs, "message block must start")
			firstLine := strings.SplitN(strings.TrimRight(tc.emit, "\n"), "\n", 2)[0]
			assert.Contains(t, obs, "  1 | "+firstLine)
			assert.Contains(t, obs, "move `m` to its own physical line")
		})
	}
}

func TestRunLoop_HeredocSetupMessageLookingTextExecutesAsBash(t *testing.T) {
	t.Parallel()
	emit := "cat <<EOF m\"Done.\"\nEOF\n"
	model := &scriptedModel{emits: []string{emit, "exit"}}
	runner := &fakeRunner{results: []ExecResult{{ExitCode: 0}}}
	deps, ms := newDeps(t, model, runner, nil)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Equal(t, []string{emit}, runner.bash)

	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	assert.Equal(t, emit, results[0].CommandContent().Command)
}

func TestRunLoop_NestedMessageBlockLookingTextExecutesAsBash(t *testing.T) {
	t.Parallel()
	emit := "if true; then\n  m\"Done.\"\nfi\n"
	model := &scriptedModel{emits: []string{emit, "exit"}}
	runner := &fakeRunner{results: []ExecResult{{ExitCode: 127}}}
	deps, ms := newDeps(t, model, runner, nil)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Equal(t, []string{emit}, runner.bash)

	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	assert.Equal(t, emit, results[0].CommandContent().Command)
}

func TestRunLoop_HeredocMessageLookingTextStaysPlainBash(t *testing.T) {
	t.Parallel()
	emit := "cat <<EOF\nm\"literal\"\nEOF\n"
	model := &scriptedModel{emits: []string{emit, "exit"}}
	runner := &fakeRunner{results: []ExecResult{{Stdout: []byte("m\"literal\"\n"), ExitCode: 0}}}
	deps, ms := newDeps(t, model, runner, nil)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Equal(t, []string{emit}, runner.bash)

	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	assert.Equal(t, emit, results[0].CommandContent().Command)
}

func TestRunLoop_NaturalLanguageWithEqualsStaysBash(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"Output=$(pwd)", "exit"}}
	runner := &fakeRunner{results: []ExecResult{{Stdout: []byte("/tmp\n"), ExitCode: 0}}}
	deps, _ := newDeps(t, model, runner, nil)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Equal(t, []string{"Output=$(pwd)"}, runner.bash)
}

func TestRunLoop_NaturalLanguageFirstLineWithBashRestRewritesToMessageBlock(t *testing.T) {
	t.Parallel()
	bash := "sed -n '90,130p' internal/agent/agent_run.go\n" +
		"echo \"===SEPARATOR===\"\n" +
		"grep -n \"func publishMessageBlocks\\|func publishMixedMessageBlocks\\|func handleMessageOnlyBlocks\\|func deliverMessageBlock\" internal/agent/loop.go\n"
	emit := "Let me read the remaining key functions.\n\n" + bash
	model := &scriptedModel{emits: []string{emit, "exit"}}
	runner := &fakeRunner{results: []ExecResult{{Stdout: []byte("ok\n"), ExitCode: 0}}}
	deps, ms := newDeps(t, model, runner, nil)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	assert.Equal(t, []string{bash}, runner.bash)

	assistants := assistantsByOrder(ms)
	require.GreaterOrEqual(t, len(assistants), 1)
	assert.Equal(t, "m#\"Let me read the remaining key functions.\"#\n\n"+bash, assistants[0].Content().Text)
	for _, assistant := range assistants[1:] {
		assert.NotEqual(t, "Let me read the remaining key functions.", assistant.Content().Text)
	}
}

func TestRunLoop_NaturalLanguageShapesRePromptWithoutRunning(t *testing.T) {
	t.Parallel()
	for _, emit := range []string{
		"Done.\n:continue",
		"Done.\ngo ahead",
		"I'll inspect the repo.\nif true then",
		"我已经完成了。\n不需要继续操作。",
		"確認しました。\n次の操作は不要です。",
	} {
		t.Run(emit, func(t *testing.T) {
			t.Parallel()
			model := &scriptedModel{emits: []string{emit, "exit"}}
			runner := &fakeRunner{}
			deps, ms := newDeps(t, model, runner, nil)

			stop, err := runLoop(context.Background(), deps, nil, "")
			require.NoError(t, err)
			assert.Equal(t, stopExit, stop)
			assert.Empty(t, runner.bash)

			assistants := assistantsByOrder(ms)
			require.Len(t, assistants, 2)
			assert.Equal(t, message.FinishReasonToolUse, assistants[0].FinishReason())
			results := resultsByOrder(ms)
			require.Len(t, results, 1)
			assert.Contains(t, results[0].Content().Text, `m"`)
			assert.NotContains(t, results[0].Content().Text, "narrate")
		})
	}
}

func TestReplaceAssistantTextPreservesReasoning(t *testing.T) {
	t.Parallel()
	msg := message.Message{
		Parts: []message.ContentPart{
			message.ReasoningContent{Thinking: "thought"},
			message.TextContent{Text: "old"},
		},
	}

	replaceAssistantText(&msg, "new")

	assert.Equal(t, "thought", msg.ReasoningContent().Thinking)
	assert.Equal(t, "new", msg.Content().Text)
	require.Len(t, msg.Parts, 2)
}

// TestRunLoop_DrainOnToolCall ensures queued user input still drains after the
// tool-call re-prompt branch, matching other non-exec re-prompt paths.
func TestRunLoop_DrainOnToolCall(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"[tool_call]{\"name\":\"bash\"}[/tool_call]", "exit"}}
	rec := &recordingRecorder{}
	deps, ms := newDepsWithDrain(t, model, &fakeRunner{}, rec, cannedDrainer([]string{"q1"}))

	_, err := runLoop(context.Background(), deps, nil, "go")
	require.NoError(t, err)

	results := messagesByRole(ms, message.Result)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Content().Text, alertPrefix)
	assert.Contains(t, results[0].Content().Text, "There is NO tool/function calling API")
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1)
	assert.Equal(t, "q1", users[0].Content().Text)
}

// --- SSOT equivalence tests ---

// TestObservationSSOT_EmptySuccess proves a command with no output carries
// the same Observation body text that formatResultForModel produces, and
// that replay via FormatResults wraps it in a single <result> envelope
// matching the live observation.
func TestObservationSSOT_EmptySuccess(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"echo -n", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{ExitCode: 0, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	_, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)

	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	cc := results[0].CommandContent()

	// Observation should be the body of formatResultForModel output.
	expectedBody := "Bash completed with no output"
	assert.Equal(t, expectedBody, cc.Observation,
		"Observation must match formatResultForModel body for empty output")

	// FormatResults should produce the same envelope the live loop sends.
	envelope := message.FormatResults([]message.CommandContent{cc})
	assert.Equal(t, "<result>\n"+expectedBody+"\n</result>", envelope,
		"FormatResults must wrap Observation in single <result> envelope")

	// Legacy fallback fields still set correctly.
	assert.Equal(t, "echo -n", cc.Command)
	assert.Equal(t, "", cc.Output)
	require.NotNil(t, cc.ExitCode)
	assert.Equal(t, 0, *cc.ExitCode)
}

func TestRunLoop_RunnerErrorIsPersistedForModel(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"pwd", "exit"}}
	runner := &fakeRunner{results: []ExecResult{{
		ExitCode: -1,
		Err:      errors.New(`temenos: daemon returned HTTP 400: {"error":"validation error: path must be absolute: \".cursor/rules/\""}`),
	}}}
	deps, ms := newDeps(t, model, runner, nil)

	_, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)

	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	cc := results[0].CommandContent()
	require.NotNil(t, cc.ExitCode)
	assert.Equal(t, -1, *cc.ExitCode)
	assert.Contains(t, cc.Output, "temenos: daemon returned HTTP 400")
	assert.Contains(t, cc.Observation, "path must be absolute")
	assert.NotContains(t, cc.Observation, "Bash completed with no output")
}

// TestObservationSSOT_FailureWithStderr proves a failing command's
// Observation includes stderr text and exit code appendix, and replay
// matches live.
func TestObservationSSOT_FailureWithStderr(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"ls /nonexistent", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stderr: []byte("ls: /nonexistent: No such file or directory\n"), ExitCode: 1, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	_, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)

	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	cc := results[0].CommandContent()

	// Observation includes stderr text and exit code.
	assert.Contains(t, cc.Observation, "ls: /nonexistent: No such file or directory")
	assert.Contains(t, cc.Observation, "(exit code: 1)")

	// Replay via FormatResults wraps body in envelope.
	envelope := message.FormatResults([]message.CommandContent{cc})
	// The envelope should contain the observation body inside <result> tags.
	wantHead := "<result>\n" + cc.Observation + "\n</result>"
	assert.Equal(t, wantHead, envelope,
		"FormatResults must use Observation verbatim, not fall back to Output")

	// Legacy fields still set.
	assert.Equal(t, "ls /nonexistent", cc.Command)
	assert.NotEmpty(t, cc.Output)
}

// TestObservationSSOT_CmdNotFound proves that when bash prints "command not found"
// the result row is NOT persisted. Instead, the combined rePrompt + envelope is
// persisted as a single TextContent User message. Replay via ToAIMessage reads it
// back as one User message with the exact observation text.
func TestObservationSSOT_CmdNotFound(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"xyzzy", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stderr: []byte("bash: xyzzy: command not found\n"), ExitCode: 127, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	// Result row is kept with exit code 1 (not abandoned).
	results := resultsByOrder(ms)
	require.Len(t, results, 1, "result row must exist for cmd-not-found")
	cc := results[0].CommandContent()
	assert.Equal(t, "xyzzy", cc.Command)
	require.NotNil(t, cc.ExitCode)
	obs := cc.Observation
	assert.Equal(t, 1, *cc.ExitCode, "cmd-not-found result has exit code 1")
	assert.False(t, cc.Pending)

	// Verify Observation contains the re-prompt text (alert prefix + word).
	assert.Contains(t, obs, "[ALERT from runtime]")
	assert.Contains(t, obs, "`xyzzy`")
	assert.Contains(t, obs, "command not found")

	// Verify it contains the stderr in the envelope body (exit code 127).
	assert.Contains(t, obs, "STDERR:")
	assert.Contains(t, obs, "bash: xyzzy: command not found")
	assert.Contains(t, obs, "(exit code: 127)")

	// No separate User message for cmd-not-found re-prompt.
	users := messagesByRole(ms, message.User)
	assert.Empty(t, users, "cmd-not-found must NOT persist a separate User message")
}

// TestObservationSSOT_RePromptRoundTrip verifies that each re-prompt type
// persists as a TextContent User message and replays identically through
// ToAIMessage, matching the original rePromptX() function output.
func TestObservationSSOT_RePromptRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		obs  string
		emit string
	}{
		{
			name: "empty",
			obs:  rePromptEmpty(),
			emit: "",
		},
		{
			name: "tool-call",
			obs:  rePromptToolCall(),
			emit: "```\nsome\n```",
		},
		{
			name: "invalid-bash",
			obs:  rePromptInvalidBash("syntax error near unexpected token"),
			emit: "echo 'unclosed",
		},
		{
			name: "banned",
			obs:  rePromptBlockedPattern(),
			emit: "sed -i 's/foo/bar/g' file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Persist the observation as a User-role TextContent message.
			ms := newMockMessageService()
			msg, err := ms.Create(context.Background(), "s-test", message.CreateMessageParams{
				Role:  message.User,
				Parts: []message.ContentPart{message.TextContent{Text: tt.obs}},
			})
			require.NoError(t, err)

			// Read back through ToAIMessage.
			aiMsgs := msg.ToAIMessage()
			require.Len(t, aiMsgs, 1)
			assert.Equal(t, fantasy.MessageRoleUser, aiMsgs[0].Role)
			require.Len(t, aiMsgs[0].Content, 1)
			tp, ok := aiMsgs[0].Content[0].(fantasy.TextPart)
			require.True(t, ok, "expected TextPart")
			assert.Equal(t, tt.obs, tp.Text,
				"re-prompt %q: ToAIMessage must preserve observation verbatim", tt.name)
		})
	}
}

// recordingRecorder implements recorderIface for test assertions.
type recordingRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordingRecorder) record(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, s)
}

func (r *recordingRecorder) Open(context.Context) error { return nil }

func (r *recordingRecorder) UserMessage(_ context.Context, _, text string) error {
	r.record("UserMessage:" + truncate(text, 30))
	return nil
}

func (r *recordingRecorder) AgentEmit(_ context.Context, _, emit string) error {
	r.record("AgentEmit:" + truncate(emit, 30))
	return nil
}

func (r *recordingRecorder) ProseMessage(_ context.Context, _, text string) error {
	r.record("ProseMessage:" + truncate(text, 30))
	return nil
}

func (r *recordingRecorder) BashResult(_ context.Context, _ string, out []byte, exitCode int, _ time.Duration) error {
	r.record("BashResult:" + truncate(string(out), 20) + ":exit=" + itoa(exitCode))
	return nil
}

func (r *recordingRecorder) BashSkipped(_ context.Context, _ string, sev, desc string) error {
	r.record("BashSkipped:" + sev + ":" + desc)
	return nil
}

func (r *recordingRecorder) RuntimeEvent(_ context.Context, _ string, sev, desc string) error {
	r.record("RuntimeEvent:" + sev + ":" + desc)
	return nil
}

func (r *recordingRecorder) TurnEnd(context.Context, string) error {
	r.record("TurnEnd")
	return nil
}

func (r *recordingRecorder) Close() error { return nil }

// local interface — no longer needs transcript import

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func itoa(i int) string { return strings.TrimSpace(intString(i)) }

func intString(i int) string {
	// avoid strconv import
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	n := len(buf)
	for i > 0 {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		buf[n] = '-'
	}
	return string(buf[n:])
}
