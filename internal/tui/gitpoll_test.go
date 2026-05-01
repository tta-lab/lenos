package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tta-lab/lenos/internal/workspace"
)

type mockGitWS struct {
	inWorktree bool
	files      []workspace.ModifiedFile
	listErr    error
}

func (m *mockGitWS) IsGitWorktree(ctx context.Context) bool { return m.inWorktree }
func (m *mockGitWS) ListModifiedFiles(ctx context.Context) ([]workspace.ModifiedFile, error) {
	return m.files, m.listErr
}

func TestStartGitPoll_SkippedOutsideWorktree(t *testing.T) {
	ws := &mockGitWS{inWorktree: false}
	p, cmd := StartGitPoll(ws)
	assert.Nil(t, p, "poller is nil outside a worktree")
	assert.Nil(t, cmd, "no cmd outside a worktree")
}

func TestGitPoller_EmitsFilesWhenInWorktree(t *testing.T) {
	ws := &mockGitWS{
		inWorktree: true,
		files: []workspace.ModifiedFile{
			{Path: "a.go", Added: 3, Deleted: 1},
			{Path: "b.go", Added: 0, Deleted: 5},
		},
	}
	p, cmd := StartGitPoll(ws)
	require.NotNil(t, p)
	require.NotNil(t, cmd, "first tick is scheduled")
	defer p.Stop()

	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	select {
	case msg := <-done:
		poll, ok := msg.(GitPollMsg)
		require.True(t, ok, "tick emits GitPollMsg, got %T", msg)
		assert.Len(t, poll.Files, 2)
		assert.Equal(t, "a.go", poll.Files[0].Path)
	case <-time.After(4 * time.Second):
		t.Fatal("GitPoller did not emit within 4s")
	}
}

func TestGitPoller_ListErrEmitsNilFiles(t *testing.T) {
	ws := &mockGitWS{
		inWorktree: true,
		listErr:    errors.New("git unreachable"),
	}
	p, cmd := StartGitPoll(ws)
	require.NotNil(t, p)
	defer p.Stop()

	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	select {
	case msg := <-done:
		poll, ok := msg.(GitPollMsg)
		require.True(t, ok)
		assert.Nil(t, poll.Files, "errors swallowed → nil Files")
	case <-time.After(4 * time.Second):
		t.Fatal("GitPoller did not emit within 4s")
	}
}

func TestGitPoller_StopHaltsEmissions(t *testing.T) {
	ws := &mockGitWS{inWorktree: true}
	p, _ := StartGitPoll(ws)
	require.NotNil(t, p)

	p.Stop()

	assert.Nil(t, p.WaitNext(), "WaitNext returns nil after Stop")
	assert.NotPanics(t, func() { p.Stop() })
}
