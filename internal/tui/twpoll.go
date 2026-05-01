package tui

import (
	"context"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/taskwarrior"
)

// TwPollMsg carries the latest taskwarrior subtask list to the UI.
type TwPollMsg struct {
	Todos []session.Todo
}

// TwPoller wraps a 500ms ticker that emits TwPollMsg via WaitNext.
// Lifecycle is tied to the UI process; one poller per worker.
type TwPoller struct {
	ticker *time.Ticker
	jobID  string
	stopCh chan struct{}
}

// StartTwPoll begins polling taskwarrior subtasks under jobID. Performs a
// synchronous initial poll so the first TwPollMsg lands without waiting for
// the first tick. Returns (nil, nil) when jobID is empty (non-TW workers).
//
// The returned tea.Cmd carries the initial Todos directly — call WaitNext for
// every subsequent tick.
func StartTwPoll(jobID string) (*TwPoller, tea.Cmd) {
	if jobID == "" {
		return nil, nil
	}

	p := &TwPoller{
		ticker: time.NewTicker(500 * time.Millisecond),
		jobID:  jobID,
		stopCh: make(chan struct{}),
	}

	todos, err := taskwarrior.PollSubtasks(context.Background(), jobID)
	if err != nil {
		slog.Warn("Initial TW poll failed", "err", err, "jobID", jobID)
		todos = nil
	}

	initial := func() tea.Msg { return TwPollMsg{Todos: todos} }
	return p, initial
}

// WaitNext returns a tea.Cmd that blocks until the next tick (or Stop) and
// emits a TwPollMsg with freshly-polled todos. A nil poller yields a nil cmd.
func (p *TwPoller) WaitNext() tea.Cmd {
	if p == nil || p.ticker == nil {
		return nil
	}
	ticker := p.ticker
	stopCh := p.stopCh
	jobID := p.jobID
	return func() tea.Msg {
		select {
		case <-ticker.C:
		case <-stopCh:
			return nil
		}
		todos, err := taskwarrior.PollSubtasks(context.Background(), jobID)
		if err != nil {
			slog.Warn("TW poll failed", "err", err, "jobID", jobID)
			return TwPollMsg{Todos: nil}
		}
		return TwPollMsg{Todos: todos}
	}
}

// Stop halts the ticker and unblocks any pending WaitNext command.
func (p *TwPoller) Stop() {
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
