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
	mu     sync.Mutex
	emits  []string
	usages []fantasy.Usage // optional: per-emit usage override; default Usage{1,1}
	errOn  []int           // call indices (pre-increment) where Stream yields an error
	calls  int
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

func (r *recordingRecorder) AgentEmit(_ context.Context, _, emit string) (transcript.TrailerToken, error) {
	r.record("AgentEmit:" + truncate(emit, 30))
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
	assert.Equal(t, []string{"UserMessage:do nothing", "AgentEmit:exit", "BashSkipped:normal:exit — turn ends", "TurnEnd"}, rec.calls)

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

	// Recorder saw original prompt → announce → result → exit announce → turn-end.
	assert.Equal(t, []string{
		"UserMessage:say hi",
		"AgentEmit:echo hi",
		"BashResult:hi\n:exit=0",
		"AgentEmit:exit",
		"BashSkipped:normal:exit — turn ends",
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

	// calls[1] is AgentEmit; calls[2] is BashSkipped; final is TurnEnd.
	require.Contains(t, rec.calls[2], "BashSkipped:normal:empty emit")
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

	// calls[1] is AgentEmit; calls[2] is BashSkipped.
	require.Contains(t, rec.calls[2], "BashSkipped:warn:invalid bash")
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
	assert.Contains(t, rec.calls[1], "AgentEmit:sed -i")
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

// TestRunLoop_ExecExitSingleEmit covers the classifyExecExit happy path:
// the model emits `cmd && exit` once, the runner executes the cmd, and
// the loop calls TurnEnd + returns stopExit WITHOUT re-prompting the
// model (verified by scriptedModel containing only the single emit —
// any extra Stream call would panic).
func TestRunLoop_ExecExitSingleEmit(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{`narrate "hi" && exit`}}
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: []byte("hi\n"), ExitCode: 0, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "say hi")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop, "single-emit cmd && exit must end the turn")

	// Recorder sequence proves no second model emit happened: prompt,
	// announce, result, turn-end. A regression that re-prompts after the
	// run would insert another announce/result pair.
	assert.Equal(t, []string{
		"UserMessage:say hi",
		`AgentEmit:narrate "hi" && exit`,
		"BashResult:hi\n:exit=0",
		"TurnEnd",
	}, rec.calls)

	// Bash actually ran, and the assistant row finished EndTurn.
	require.Equal(t, []string{`narrate "hi" && exit`}, runner.bash)
	assistants := assistantsByOrder(ms)
	require.Len(t, assistants, 1)
	assert.Equal(t, message.FinishReasonEndTurn, assistants[0].FinishReason())
}

// TestRunLoop_ExecExitSingleEmit_PreCmdFails pins the current behavior
// when the pre-exit command exits non-zero (e.g. `false && exit` —
// bash short-circuits so `exit` never runs, but the loop classified
// the emit as classifyExecExit and ends the turn unconditionally based
// on intent, not realised exit). If a future change wants to re-prompt
// on pre-cmd failure, this test will need to flip — making the change
// visible in code review.
func TestRunLoop_ExecExitSingleEmit_PreCmdFails(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{`false && exit`}}
	runner := &fakeRunner{results: []ExecResult{
		{ExitCode: 1, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, _ := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop, "classifyExecExit honors model intent regardless of pre-cmd exit code")

	// Turn ended despite the non-zero exit: TurnEnd appears in the trace
	// and no second model emit was solicited (would have panicked).
	assert.Contains(t, rec.calls, "TurnEnd")
	assert.Equal(t, "TurnEnd", rec.calls[len(rec.calls)-1])
}

// TestRunLoop_ExecExitSingleEmit_CmdNotFound_NoRePrompt pins the routing
// behavior for prose-shaped emits that happen to end with `&& exit`. With the
// classifyProsePrefix gate active, "Let me start && exit" is caught as prose
// BEFORE trailingExitRe can match — bash never runs, a prose re-prompt fires,
// and the model must re-emit. If a future change wants to let classifyExecExit
// win again, this test must flip too — making the change visible in review.
func TestRunLoop_ExecExitSingleEmit_CmdNotFound_NoRePrompt(t *testing.T) {
	t.Parallel()
	// Two emits: prose-prefix re-prompt fires on the first, model corrects with exit.
	model := &scriptedModel{emits: []string{`Let me start && exit`, "exit"}}
	runner := &fakeRunner{} // bash must NOT be called — prose gate fires pre-exec
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	// Prose gate fired — no exec, no result row.
	assert.Empty(t, runner.bash, "prose-prefix branch must not invoke runner")
	results := messagesByRole(ms, message.Result)
	assert.Empty(t, results, "no result row — bash bypassed by prose-prefix gate")

	// Re-prompt persisted for the prose-shape failure.
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1)
	assert.Contains(t, users[0].Content().Text, "[ALERT from runtime]")
	assert.Contains(t, users[0].Content().Text, "`Let`")
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
	assert.Equal(t, []string{"UserMessage:hello world", "AgentEmit:exit", "BashSkipped:normal:exit — turn ends", "TurnEnd"}, rec.calls)
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
		"AgentEmit:echo hi",
		"BashResult:hi\n:exit=0",
		"AgentEmit:exit",
		"BashSkipped:normal:exit — turn ends",
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
		"AgentEmit:echo hi",
		"BashResult:hi\n:exit=0",
		"UserMessage:follow up",
		"AgentEmit:exit",
		"BashSkipped:normal:exit — turn ends",
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
		"AgentEmit:echo hi",
		"BashResult:hi\n:exit=0",
		"UserMessage:m1",
		"UserMessage:m2",
		"UserMessage:m3",
		"AgentEmit:exit",
		"BashSkipped:normal:exit — turn ends",
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
	// order: original prompt, AgentEmit, BashSkipped, drained followup, exit announce, BashSkipped, turn-end.
	require.Contains(t, rec.calls[2], "BashSkipped:normal:empty emit")
	require.Contains(t, rec.calls[3], "UserMessage:q1")
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
	require.Contains(t, rec.calls[2], "BashSkipped:warn:invalid bash")
	require.Contains(t, rec.calls[3], "UserMessage:q1")
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
	require.Contains(t, rec.calls[1], "AgentEmit:sed -i")
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
	require.Contains(t, rec.calls[1], "AgentEmit:sleep 5")
	require.Contains(t, rec.calls[2], "BashResult:")
	require.Contains(t, rec.calls[3], "RuntimeEvent:warn:timeout")
	require.Contains(t, rec.calls[4], "UserMessage:q1")
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 2)
	assert.Contains(t, users[0].Content().Text, "[runtime]")
	assert.Equal(t, "q1", users[1].Content().Text)
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

	require.Contains(t, rec.calls[1], "AgentEmit:nopebinary")
	require.Contains(t, rec.calls[2], "BashResult:")
	require.Contains(t, rec.calls[3], "RuntimeEvent:warn:")
	require.Contains(t, rec.calls[3], "command not found")
	require.Contains(t, rec.calls[4], "UserMessage:q1")
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 2)
	assert.Contains(t, users[0].Content().Text, "[ALERT from runtime]")
	assert.Equal(t, "q1", users[1].Content().Text)
}

func (m *errorStreamModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	panic("not used")
}

var _ fantasy.LanguageModel = (*errorStreamModel)(nil)

func TestRunLoop_OnUsageStopReturnsShouldSummarize(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"echo a", "echo b"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: []byte("a\n"), ExitCode: 0, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)
	var calls int
	deps.onUsage = func(_ int, _ fantasy.Usage, _ fantasy.ProviderMetadata) bool {
		calls++
		return calls >= 2 // request compact on second step
	}

	stop, err := runLoop(context.Background(), deps, nil, "do work")
	require.NoError(t, err)
	assert.Equal(t, stopShouldSummarize, stop)
	assert.Equal(t, 2, calls, "onUsage should fire once per emit, including the triggering one")

	assistants := assistantsByOrder(ms)
	require.NotEmpty(t, assistants)
	assert.Equal(t, message.FinishReasonEndTurn, assistants[len(assistants)-1].FinishReason())

	var sawTurnEnd, sawAutoCompactEvent bool
	for _, c := range rec.calls {
		if c == "TurnEnd" {
			sawTurnEnd = true
		}
		if strings.HasPrefix(c, "RuntimeEvent:warn:auto-compact:") {
			sawAutoCompactEvent = true
		}
	}
	assert.True(t, sawTurnEnd, "expected recorder.TurnEnd on auto-compact stop")
	assert.True(t, sawAutoCompactEvent, "expected recorder.RuntimeEvent for auto-compact")
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

	// Re-prompt observation persisted.
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1)
	assert.Contains(t, users[0].Content().Text, "`lorem`", "re-prompt must reference stderr token")
	assert.Contains(t, users[0].Content().Text, "command not found")

	// RuntimeEvent logged — mentions the captured token.
	var sawRePromptEvent bool
	for _, c := range rec.calls {
		if strings.Contains(c, "command not found") && strings.Contains(c, "lorem") {
			sawRePromptEvent = true
			break
		}
	}
	assert.True(t, sawRePromptEvent, "RuntimeEvent must mention token and command not found")
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

	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1, "exactly one User message (the re-prompt) must be persisted")
	assert.Contains(t, users[0].Content().Text, "[ALERT from runtime]")
	assert.Contains(t, users[0].Content().Text, "`unknowncmd`")
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

	// Re-prompt User row present.
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1)
	assert.Contains(t, users[0].Content().Text, "command not found")
}

// TestRunLoop_Exit127_ProseRePrompts tests that when the model emits English
// prose and bash prints "command not found", the re-prompt includes guidance
// about prose being parsed as bash.
func TestRunLoop_Exit127_ProseRePrompts(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"Hello world", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stderr: []byte("bash: Hello: command not found\n"), ExitCode: 127, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "find")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1)
	obs := users[0].Content().Text
	assert.Contains(t, obs, "`Hello`")
	assert.Contains(t, obs, "narrate <<'EOF'")
}

// TestRunLoop_CmdNotFound_RePromptIncludesFenceGuidance tests that the
// rePromptCmdNotFound template includes guidance about markdown fences
// regardless of input shape.
func TestRunLoop_CmdNotFound_RePromptIncludesFenceGuidance(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"```bash\necho hi\n```", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stderr: []byte("bash: bash: command not found\n"), ExitCode: 127, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	stop, err := runLoop(context.Background(), deps, nil, "run")
	require.NoError(t, err)
	assert.Equal(t, stopExit, stop)

	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1)
	obs := users[0].Content().Text
	assert.Contains(t, obs, "markdown fence")
	assert.Contains(t, obs, "```bash")
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

// TestRunLoop_InvalidBash_EmitVisibleInTranscript pins that the model's actual
// output is recorded BEFORE the runtime warning when bash -n rejects.
// Regression guard against the visibility gap closed by lifting AgentEmit.
func TestRunLoop_InvalidBash_EmitVisibleInTranscript(t *testing.T) {
	t.Parallel()
	badEmit := "if true then  # missing fi"
	model := &scriptedModel{emits: []string{badEmit, "exit"}}
	rec := &recordingRecorder{}
	deps, _ := newDeps(t, model, &fakeRunner{}, rec)

	_, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)

	// Recorder sequence: emit announce must come BEFORE the bash skipped event.
	emitIdx, skippedIdx := -1, -1
	for i, c := range rec.calls {
		if strings.HasPrefix(c, "AgentEmit:") && strings.Contains(c, "if true then") {
			emitIdx = i
		}
		if strings.Contains(c, "BashSkipped:warn:invalid bash") {
			skippedIdx = i
		}
	}
	require.GreaterOrEqual(t, emitIdx, 0, "emit must be announced — visibility gap regression")
	require.GreaterOrEqual(t, skippedIdx, 0)
	assert.Less(t, emitIdx, skippedIdx, "emit announce must come before bash skipped event in transcript")
}

// TestRunLoop_Empty_EmitVisibleInTranscript is the same regression guard for empty emits.
func TestRunLoop_Empty_EmitVisibleInTranscript(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"   ", "exit"}}
	rec := &recordingRecorder{}
	deps, _ := newDeps(t, model, &fakeRunner{}, rec)

	_, err := runLoop(context.Background(), deps, nil, "noop")
	require.NoError(t, err)

	var sawEmit bool
	for _, c := range rec.calls {
		if strings.HasPrefix(c, "AgentEmit:") {
			sawEmit = true
		}
	}
	assert.True(t, sawEmit, "even empty emits must be announced for SSOT")
}

// TestRunLoop_ProseThenCommand_StderrMatch_FiresRePrompt covers the stderr-scan
// failure mode from session 1bd0d74e: a multi-line emit whose first token is
// NOT Title-Cased (so classifyProsePrefix doesn't fire), but bash reports
// "command not found" on an internal line and the trailing real command exits 0,
// making the overall exit 0. The old exit-127 gate missed these; stderr-scan catches them.
//
// Note: Title-Cased-first-word emits like "The PR already exists..." now route
// to classifyProsePrefix pre-exec and never reach the stderr-scan path.
func TestRunLoop_ProseThenCommand_StderrMatch_FiresRePrompt(t *testing.T) {
	t.Parallel()
	// Emit starts with lowercase (bypasses classifyProsePrefix) but contains a
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

	// Re-prompt must fire despite overall exit 0.
	users := messagesByRole(ms, message.User)
	require.GreaterOrEqual(t, len(users), 1)
	assert.Contains(t, users[0].Content().Text, "`nonexistentcmd`", "re-prompt must capture the first not-found token from stderr")

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
	alertIdx := strings.Index(obs, "[ALERT from runtime]")
	envelopeIdx := strings.Index(obs, "<result>")
	require.GreaterOrEqual(t, alertIdx, 0, "alert prefix must be present")
	require.GreaterOrEqual(t, envelopeIdx, 0, "result envelope must be present")
	assert.Less(t, alertIdx, envelopeIdx, "alert MUST appear before envelope (salience flip)")

	// Result row also exists (envelope preserved, not replaced).
	results := resultsByOrder(ms)
	require.Len(t, results, 1)
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
// Note: Title-Cased-first-word emits like "The PR already exists..." now route to
// classifyProsePrefix pre-exec; this test uses a lowercase-starting emit so it
// exercises the post-exec stderr-scan path.
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
	alertIdx := strings.Index(rePrompt, "[ALERT from runtime]")
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
	model := &scriptedModel{emits: []string{"```bash\ngit log\n```", "exit"}}
	runner := &fakeRunner{results: []ExecResult{
		{Stderr: []byte("bash: line 2: 652a5f45: command not found\n"), ExitCode: 127, Duration: time.Millisecond},
	}}
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	_, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)

	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1, "re-prompt MUST fire on multi-line bash error format")
	obs := users[0].Content().Text
	assert.Contains(t, obs, "[ALERT from runtime]")
	assert.Contains(t, obs, "`652a5f45`",
		"must capture the offending token even when stderr has 'line N:' prefix")
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

// TestRunLoop_ProsePrefix_FiresPreExec verifies a Title-Cased prose-first-word
// emit triggers rePromptProsePrefix BEFORE bash exec, with no result row
// created (the runtime bypasses bash entirely for this shape).
func TestRunLoop_ProsePrefix_FiresPreExec(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"Read the files I need", "exit"}}
	runner := &fakeRunner{} // no canned results — runner must NOT be called
	rec := &recordingRecorder{}
	deps, ms := newDeps(t, model, runner, rec)

	_, err := runLoop(context.Background(), deps, nil, "")
	require.NoError(t, err)

	// No exec ran — runner.bash should be empty.
	assert.Empty(t, runner.bash, "prose-prefix branch must not invoke runner")

	// No result row created.
	results := messagesByRole(ms, message.Result)
	assert.Empty(t, results, "no result row when prose-prefix bypasses exec")

	// Re-prompt persisted as User row.
	users := messagesByRole(ms, message.User)
	require.Len(t, users, 1)
	obs := users[0].Content().Text
	assert.Contains(t, obs, "[ALERT from runtime]")
	assert.Contains(t, obs, "`Read`")
}

// TestRunLoop_ProsePrefix_LowercaseAccepted verifies a lowercase first word
// (legit bash command) does NOT trigger the pre-exec re-prompt — it routes
// through classifyExec normally.
func TestRunLoop_ProsePrefix_LowercaseAccepted(t *testing.T) {
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

// TestRunLoop_DrainOnProsePrefix is the drain peer for the prose-prefix branch.
// Every re-prompt branch must have a DrainOn<X> counterpart so a future
// simplification can't silently drop drainAndAppend.
func TestRunLoop_DrainOnProsePrefix(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"Now starting the task", "exit"}}
	rec := &recordingRecorder{}
	deps, ms := newDepsWithDrain(t, model, &fakeRunner{}, rec, cannedDrainer([]string{"q1"}))

	_, err := runLoop(context.Background(), deps, nil, "go")
	require.NoError(t, err)

	users := messagesByRole(ms, message.User)
	require.Len(t, users, 2)
	assert.Contains(t, users[0].Content().Text, "[ALERT from runtime]")
	assert.Contains(t, users[0].Content().Text, "`Now`", "must reference the offending first word")
	assert.Equal(t, "q1", users[1].Content().Text)
}
