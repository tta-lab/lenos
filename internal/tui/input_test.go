package tui

import (
	"context"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/ui/common"
	"github.com/tta-lab/lenos/internal/workspace"
)

// agentRunRecorder is a stripped-down Workspace double that captures every
// AgentRun call and panics if anything else is touched — by panicking via
// the embedded nil interface, it asserts the H6 invariant: Submit must not
// reach for recorder.UserMessage or any other workspace method.
type agentRunRecorder struct {
	workspace.Workspace
	mu     sync.Mutex
	calls  []agentRunCall
	runErr error
	cfgRet any
}

type agentRunCall struct {
	sessionID string
	prompt    string
}

func (r *agentRunRecorder) AgentRun(ctx context.Context, sessionID, prompt string, _ ...message.Attachment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, agentRunCall{sessionID: sessionID, prompt: prompt})
	return r.runErr
}

func (r *agentRunRecorder) snapshot() []agentRunCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]agentRunCall, len(r.calls))
	copy(out, r.calls)
	return out
}

func newConfiguredInputPane(ws workspace.Workspace, sessionID string) *inputPane {
	p := newInputPane()
	p.Configure(&common.Common{Workspace: ws}, sessionID)
	return p
}

func TestInputPane_TextareaFocusBlurRouting(t *testing.T) {
	p := newConfiguredInputPane(&agentRunRecorder{}, "sess-1")

	require.True(t, p.IsFocused(), "newInputPane focuses the textarea by default")

	p.Blur()
	assert.False(t, p.IsFocused())

	cmd := p.Focus()
	_ = cmd
	assert.True(t, p.IsFocused(), "Focus restores the cursor")
}

func TestInputPane_SubmitCallsAgentRun(t *testing.T) {
	rec := &agentRunRecorder{}
	p := newConfiguredInputPane(rec, "sess-42")
	p.textarea.SetValue("ship the feature")

	cmd := p.Submit()
	require.NotNil(t, cmd, "Submit returns a tea.Cmd that runs AgentRun")
	cmd() // invoke

	calls := rec.snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, "sess-42", calls[0].sessionID)
	assert.Equal(t, "ship the feature", calls[0].prompt)
	assert.Empty(t, p.textarea.Value(), "textarea is reset after submit")
}

func TestInputPane_SubmitDoesNotCallRecorderUserMessage(t *testing.T) {
	// agentRunRecorder embeds a nil workspace.Workspace — any method other
	// than AgentRun panics. The H6 invariant says Submit only calls AgentRun;
	// if a regression added e.g. ListMessages or anything via a recorder
	// path, this test would panic. It's a structural negative assertion.
	rec := &agentRunRecorder{}
	p := newConfiguredInputPane(rec, "sess-1")
	p.textarea.SetValue("hello")

	cmd := p.Submit()
	require.NotNil(t, cmd)
	cmd() // panics if Submit touches anything beyond AgentRun
}

func TestInputPane_EnterRoutesToSubmit(t *testing.T) {
	rec := &agentRunRecorder{}
	p := newConfiguredInputPane(rec, "sess-1")
	p.textarea.SetValue("typed line")

	cmd := p.Update(testKeyMsg{text: "enter"})
	require.NotNil(t, cmd, "Enter routes to Submit which yields a non-nil cmd")
	cmd()

	calls := rec.snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, "typed line", calls[0].prompt)
}

func TestInputPane_SubmitEmptyIsNoop(t *testing.T) {
	rec := &agentRunRecorder{}
	p := newConfiguredInputPane(rec, "sess-1")
	cmd := p.Submit()
	assert.Nil(t, cmd, "empty buffer → no command, no AgentRun call")
	assert.Empty(t, rec.snapshot())
}

// keep tea.Cmd import alive even if all uses inline.
var _ tea.Cmd = func() tea.Msg { return nil }
