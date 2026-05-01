package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartTwPoll_EmptyJobIDReturnsNilPoller(t *testing.T) {
	p, cmd := StartTwPoll("")
	assert.Nil(t, p, "empty jobID returns nil poller")
	assert.Nil(t, cmd, "empty jobID returns nil cmd")
}

func TestStartTwPoll_WithJobIDReturnsInitialCmd(t *testing.T) {
	// Synchronous initial poll runs the `task` CLI. With a non-existent jobID
	// the CLI returns no subtasks (or fails fast if `task` is missing); both
	// paths surface a TwPollMsg with empty Todos.
	p, cmd := StartTwPoll("nonexistent-fake-job-id")
	require.NotNil(t, p, "non-empty jobID returns a poller")
	require.NotNil(t, cmd, "non-empty jobID returns an initial cmd")
	defer p.Stop()

	msg := cmd()
	poll, ok := msg.(TwPollMsg)
	require.True(t, ok, "initial cmd emits TwPollMsg, got %T", msg)
	assert.Empty(t, poll.Todos)
}

func TestTwPoller_WaitNextEmitsTwPollMsg(t *testing.T) {
	p, _ := StartTwPoll("nonexistent-fake-job-id")
	require.NotNil(t, p)
	defer p.Stop()

	cmd := p.WaitNext()
	require.NotNil(t, cmd, "WaitNext returns a non-nil cmd while poller is live")

	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	select {
	case msg := <-done:
		_, ok := msg.(TwPollMsg)
		assert.True(t, ok, "WaitNext emits TwPollMsg, got %T", msg)
	case <-time.After(15 * time.Second):
		t.Fatal("WaitNext did not emit within 15s — exec or ticker stuck")
	}
}

func TestTwPoller_StopHaltsEmissions(t *testing.T) {
	p, _ := StartTwPoll("nonexistent-fake-job-id")
	require.NotNil(t, p)
	p.Stop()

	// After Stop, WaitNext must yield a nil cmd — no further ticks scheduled.
	assert.Nil(t, p.WaitNext(), "WaitNext returns nil after Stop")

	// Idempotent: a second Stop must not panic.
	assert.NotPanics(t, func() { p.Stop() })
}
