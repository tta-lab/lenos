package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tta-lab/lenos/internal/agent/lenosbash"
	"github.com/tta-lab/lenos/internal/message"
)

// =============================================================================
// Loop integration tests — background job path + core loop behavior
// =============================================================================

func TestRunLoop_BackgroundJobProducesRuntimeObservation(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{
		lenosbash.BashBlock("sleep 20"),
		"exit",
	}}
	runner := &fakeRunner{results: []ExecResult{
		{Background: true, JobID: "job-123", Duration: time.Second * 16},
	}}
	deps, ms := newLoopDeps(t, model, runner, nil)

	stop, err := runLoop(t.Context(), deps, nil, "run slow")
	require.NoError(t, err)
	assert.Equal(t, stopEndTurn, stop)

	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	obs := results[0].CommandContent().Observation
	assert.Contains(t, obs, "background job started (job_id: job-123)")
	assert.NotContains(t, obs, "temenos job kill")
	assert.NotContains(t, obs, "check status")
}

func TestRunLoop_ProseOnlyEndsTurn(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{"hello world\n"}}
	runner := &fakeRunner{results: []ExecResult{}}
	deps, ms := newLoopDeps(t, model, runner, nil)

	stop, err := runLoop(t.Context(), deps, nil, "test")
	require.NoError(t, err)
	assert.Equal(t, stopEndTurn, stop)

	msgs, err := ms.List(t.Context(), "s-test")
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, message.Assistant, msgs[0].Role)
	assert.Equal(t, "hello world\n", msgs[0].Content().Text)
}

func TestRunLoop_ExecThenExit(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{
		lenosbash.BashBlock("echo ok"),
		"exit",
	}}
	runner := &fakeRunner{results: []ExecResult{
		{Stdout: []byte("ok\n"), ExitCode: 0},
	}}
	deps, ms := newLoopDeps(t, model, runner, nil)

	stop, err := runLoop(t.Context(), deps, nil, "test")
	require.NoError(t, err)
	assert.Equal(t, stopEndTurn, stop)

	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	assert.Equal(t, "echo ok\n", results[0].CommandContent().Command)
	assert.Contains(t, results[0].CommandContent().Output, "ok")
}

func TestRunLoop_ContextCancel(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{emits: []string{
		lenosbash.BashBlock("sleep 99"),
	}}
	runner := &fakeRunner{results: []ExecResult{
		{Err: context.Canceled, ExitCode: -1},
	}}
	deps, ms := newLoopDeps(t, model, runner, nil)
	loopCtx, cancel := context.WithCancel(t.Context())
	cancel()

	stop, err := runLoop(loopCtx, deps, nil, "test")
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, stopCanceled, stop)
	// abandonPending marks the result row in-place; it still exists.
	results := resultsByOrder(ms)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].CommandContent().Output, "canceled before result")
}
