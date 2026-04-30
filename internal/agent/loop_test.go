package agent

import (
	"context"
	"errors"
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
	"github.com/tta-lab/lenos/internal/transcript"
)

// Compile-time guard: catches the SeverityNormal/SevNormal name skew
// mentioned in the plan-review fixup. If transcript renames, the loop and
// these tests fail to compile together.
var _ = transcript.SevNormal

// --- Test fakes ---

// scriptedModel returns a sequence of canned emits via Stream(). Each call
// to Stream consumes one entry; missing entries panic the test.
type scriptedModel struct {
	mu    sync.Mutex
	emits []string
	calls int
}

func (m *scriptedModel) Model() string    { return "test-model" }
func (m *scriptedModel) Provider() string { return "test-provider" }

func (m *scriptedModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	panic("not used")
}

func (m *scriptedModel) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls >= len(m.emits) {
		panic("scriptedModel: ran out of canned emits")
	}
	out := m.emits[m.calls]
	m.calls++
	seq := iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: out}) {
			return
		}
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, Usage: fantasy.Usage{InputTokens: 1, OutputTokens: 1}}) {
			return
		}
	})
	return seq, nil
}

func (m *scriptedModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	panic("not used")
}

func (m *scriptedModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	panic("not used")
}

var _ fantasy.LanguageModel = (*scriptedModel)(nil)

// fakeRunner returns canned ExecResults in order. Tests use it to drive
// classify=exec branches without touching /bin/bash.
type fakeRunner struct {
	mu      sync.Mutex
	results []ExecResult
	calls   int
	bash    []string
}

func (r *fakeRunner) Run(_ context.Context, bash string, _ map[string]string, _ []client.AllowedPath) ExecResult {
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

// recordingRecorder captures call sequence so tests can assert on
// announce-then-result ordering, severity, etc.
type recordingRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordingRecorder) record(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, s)
}

func (r *recordingRecorder) Open(context.Context, transcript.Meta) error { return nil }

func (r *recordingRecorder) UserMessage(_ context.Context, _, text string) error {
	r.record("UserMessage:" + truncate(text, 30))
	return nil
}

func (r *recordingRecorder) AgentBashAnnounce(_ context.Context, _, bash string) (transcript.TrailerToken, error) {
	r.record("AgentBashAnnounce:" + truncate(bash, 30))
	return transcript.TrailerToken{}, nil
}

func (r *recordingRecorder) BashResult(_ context.Context, _ transcript.TrailerToken, out []byte, exitCode int, _ time.Duration) error {
	r.record("BashResult:" + truncate(string(out), 20) + ":exit=" + itoa(exitCode))
	return nil
}

func (r *recordingRecorder) BashSkipped(_ context.Context, _ transcript.TrailerToken, sev transcript.Severity, desc string) error {
	r.record("BashSkipped:" + sevName(sev) + ":" + desc)
	return nil
}

func (r *recordingRecorder) RuntimeEvent(_ context.Context, _ string, sev transcript.Severity, desc string) error {
	r.record("RuntimeEvent:" + sevName(sev) + ":" + desc)
	return nil
}

func (r *recordingRecorder) TurnEnd(context.Context, string) error {
	r.record("TurnEnd")
	return nil
}
func (r *recordingRecorder) Close() error { return nil }

var _ transcript.Recorder = (*recordingRecorder)(nil)

func sevName(s transcript.Severity) string {
	switch s {
	case transcript.SevWarn:
		return "warn"
	case transcript.SevError:
		return "error"
	default:
		return "normal"
	}
}

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

// --- Helpers ---

func newDeps(t *testing.T, model fantasy.LanguageModel, runner Runner, rec transcript.Recorder) (loopDeps, *mockMessageService) {
	return newDepsWithDrain(t, model, runner, rec, nil)
}

func newDepsWithDrain(t *testing.T, model fantasy.LanguageModel, runner Runner, rec transcript.Recorder, drain func() []string) (loopDeps, *mockMessageService) {
	t.Helper()
	ms := newMockMessageService()
	return loopDeps{
		model:      model,
		messages:   ms,
		runner:     runner,
		recorder:   rec,
		sessionID:  "s-test",
		sysPrompt:  "you are a test",
		providerID: "test-provider-id",
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
	assert.Equal(t, []string{"UserMessage:do nothing", "TurnEnd"}, rec.calls)

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

	// Recorder saw original prompt → announce → result → turn-end.
	assert.Equal(t, []string{
		"UserMessage:say hi",
		"AgentBashAnnounce:echo hi",
		"BashResult:hi\n:exit=0",
		"TurnEnd",
	}, rec.calls)

	// DB has assistant rows + one result row, runner saw the bash.
	assert.Equal(t, []string{"echo hi"}, runner.bash)
	assert.Len(t, resultsByOrder(ms), 1)
}

func TestRunLoop_EmptyEmitRePrompts(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"   ", "exit"}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, &fakeRunner{}, rec)

	stop, err := runLoop(context.Background(), deps, nil, "noop")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	// Second call is the runtime event (first is original prompt); final is TurnEnd.
	require.Contains(t, rec.calls[1], "RuntimeEvent:normal:empty emit")
	assert.Equal(t, "TurnEnd", rec.calls[len(rec.calls)-1])

	// Observation persisted as User row.
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1)
	assert.True(t, strings.HasPrefix(users[0].Content().Text, "[runtime] your last response was empty"))
}

func TestRunLoop_InvalidBashRePrompts(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"if true then", "exit"}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, &fakeRunner{}, rec)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	// Second call is the warn-level runtime event (first is original prompt).
	require.Contains(t, rec.calls[1], "RuntimeEvent:warn:invalid bash")
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1)
	assert.Contains(t, users[0].Content().Text, "[runtime] your last response was not valid bash")
}

func TestRunLoop_BannedPatternIsAnnouncedAndSkipped(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{`sed -i 's/a/b/' f.txt`, "exit"}}
	rec := &recordingRecorder{}
	deps, _ := newDeps(t, model, &fakeRunner{}, rec)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	// Order: original prompt → announce → skipped → … → turn-end.
	require.Greater(t, len(rec.calls), 3)
	assert.Contains(t, rec.calls[1], "AgentBashAnnounce:sed -i")
	assert.Contains(t, rec.calls[2], "BashSkipped:warn:")
	assert.Equal(t, "TurnEnd", rec.calls[len(rec.calls)-1])
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

	// Result was recorded (loop must persist partial output for transcript).
	var sawResult, sawTimeoutEvent bool
	for _, c := range rec.calls {
		if strings.HasPrefix(c, "BashResult:") {
			sawResult = true
		}
		if strings.Contains(c, "RuntimeEvent:warn:timeout") {
			sawTimeoutEvent = true
		}
	}
	assert.True(t, sawResult, "BashResult must fire even on timeout")
	assert.True(t, sawTimeoutEvent, "RuntimeEvent(warn, timeout...) must fire")

	// Re-prompt observation persisted.
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1)
	assert.Contains(t, users[0].Content().Text, "exceeded the per-call timeout")
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
	deps, _ := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrStepCap))
	assert.Equal(t, stopStepCap, stop)

	// The last recorder call must be the step-cap RuntimeEvent.
	last := rec.calls[len(rec.calls)-1]
	assert.True(t, strings.HasPrefix(last, "RuntimeEvent:error:step cap"), "got %q", last)
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
	deps, _ := newDeps(t, model, &fakeRunner{}, rec)

	stop, err := runLoop(context.Background(), deps, nil, "hello world")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	// Original prompt must be recorded before any model interaction.
	assert.Equal(t, []string{"UserMessage:hello world", "TurnEnd"}, rec.calls)
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
	assert.Equal(t, []string{
		"UserMessage:test prompt",
		"AgentBashAnnounce:echo hi",
		"BashResult:hi\n:exit=0",
		"TurnEnd",
	}, rec.calls)
	// No drained rows: original prompt is recorded but not persisted by runLoop.
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
	assert.Equal(t, []string{
		"UserMessage:original",
		"AgentBashAnnounce:echo hi",
		"BashResult:hi\n:exit=0",
		"UserMessage:follow up",
		"TurnEnd",
	}, rec.calls)
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
	assert.Equal(t, []string{
		"UserMessage:original",
		"AgentBashAnnounce:echo hi",
		"BashResult:hi\n:exit=0",
		"UserMessage:m1",
		"UserMessage:m2",
		"UserMessage:m3",
		"TurnEnd",
	}, rec.calls)
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 3)
	assert.Equal(t, "m1", users[0].Content().Text)
	assert.Equal(t, "m2", users[1].Content().Text)
	assert.Equal(t, "m3", users[2].Content().Text)
}

func TestRunLoop_DrainOnEmptyEmit(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"   ", "exit"}}
	rec := &recordingRecorder{}
	deps, ms := newDepsWithDrain(t, model, &fakeRunner{}, rec, cannedDrainer([]string{"q1"}))

	stop, err := runLoop(context.Background(), deps, nil, "noop")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	// order: original prompt, runtime event, drained followup, turn-end.
	require.Contains(t, rec.calls[1], "RuntimeEvent:normal:empty emit")
	require.Contains(t, rec.calls[2], "UserMessage:q1")
	assert.Equal(t, "TurnEnd", rec.calls[len(rec.calls)-1])
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 2)
	assert.True(t, strings.HasPrefix(users[0].Content().Text, "[runtime]"))
	assert.Equal(t, "q1", users[1].Content().Text)
}

func TestRunLoop_DrainOnInvalidBash(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"if true then", "exit"}}
	rec := &recordingRecorder{}
	deps, ms := newDepsWithDrain(t, model, &fakeRunner{}, rec, cannedDrainer([]string{"q1"}))

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	require.Contains(t, rec.calls[1], "RuntimeEvent:warn:invalid bash")
	require.Contains(t, rec.calls[2], "UserMessage:q1")
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 2)
	assert.Contains(t, users[0].Content().Text, "[runtime]")
	assert.Equal(t, "q1", users[1].Content().Text)
}

func TestRunLoop_DrainOnBanned(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{`sed -i "s/a/b/" f.txt`, "exit"}}
	rec := &recordingRecorder{}
	deps, ms := newDepsWithDrain(t, model, &fakeRunner{}, rec, cannedDrainer([]string{"q1"}))

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)
	// order: original prompt, announce, skipped, drained, turn-end.
	require.Contains(t, rec.calls[1], "AgentBashAnnounce:sed -i")
	require.Contains(t, rec.calls[2], "BashSkipped:warn:")
	require.Contains(t, rec.calls[3], "UserMessage:q1")
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 2)
	assert.Contains(t, users[0].Content().Text, "[runtime]")
	assert.Equal(t, "q1", users[1].Content().Text)
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
	// order: original prompt, announce, bash result, timeout event, drained, turn-end.
	require.Contains(t, rec.calls[1], "AgentBashAnnounce:sleep 5")
	require.Contains(t, rec.calls[2], "BashResult:")
	require.Contains(t, rec.calls[3], "RuntimeEvent:warn:timeout")
	require.Contains(t, rec.calls[4], "UserMessage:q1")
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 2)
	assert.Contains(t, users[0].Content().Text, "[runtime]")
	assert.Equal(t, "q1", users[1].Content().Text)
}

func (m *errorStreamModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	panic("not used")
}

var _ fantasy.LanguageModel = (*errorStreamModel)(nil)
