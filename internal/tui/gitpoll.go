package tui

import (
	"context"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tta-lab/lenos/internal/workspace"
)

// GitWorkspace is the narrow workspace surface needed by GitPoller — exposed
// as an interface so tests can inject a mock without standing up the full
// workspace.Workspace implementation.
type GitWorkspace interface {
	IsGitWorktree(ctx context.Context) bool
	ListModifiedFiles(ctx context.Context) ([]workspace.ModifiedFile, error)
}

// GitPollMsg carries the latest set of git-modified files to the UI.
type GitPollMsg struct {
	Files []workspace.ModifiedFile
}

// GitPoller wraps a 2s ticker over GitWorkspace.ListModifiedFiles.
type GitPoller struct {
	ticker *time.Ticker
	ws     GitWorkspace
	stopCh chan struct{}
}

// StartGitPoll starts the git poller when ws.IsGitWorktree reports true.
// Returns (nil, nil) outside a worktree (no rendering work to do). The
// returned tea.Cmd carries the first scheduled tick — there is no
// synchronous initial poll, matching the legacy startGitPoll cadence.
func StartGitPoll(ws GitWorkspace) (*GitPoller, tea.Cmd) {
	if ws == nil || !ws.IsGitWorktree(context.Background()) {
		return nil, nil
	}

	p := &GitPoller{
		ticker: time.NewTicker(2 * time.Second),
		ws:     ws,
		stopCh: make(chan struct{}),
	}
	return p, p.WaitNext()
}

// WaitNext returns a tea.Cmd that emits a GitPollMsg after the next tick or
// nil after Stop. A nil poller yields a nil cmd.
func (p *GitPoller) WaitNext() tea.Cmd {
	if p == nil || p.ticker == nil {
		return nil
	}
	ticker := p.ticker
	stopCh := p.stopCh
	ws := p.ws
	return func() tea.Msg {
		select {
		case _, ok := <-ticker.C:
			if !ok {
				return GitPollMsg{Files: nil}
			}
		case <-stopCh:
			return nil
		}
		files, err := ws.ListModifiedFiles(context.Background())
		if err != nil {
			slog.Warn("Git poll failed", "err", err)
			return GitPollMsg{Files: nil}
		}
		return GitPollMsg{Files: files}
	}
}

// Stop halts the ticker and unblocks any pending WaitNext command.
func (p *GitPoller) Stop() {
	if p == nil || p.ticker == nil {
		return
	}
	p.ticker.Stop()
	select {
	case <-p.stopCh:
		// already closed
	default:
		close(p.stopCh)
	}
	p.ticker = nil
}
