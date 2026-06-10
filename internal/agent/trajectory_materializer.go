package agent

import (
	"context"
	"fmt"

	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/session"
)

// TrajectoryMaterializer reads session + messages from DB and writes the ATIF
// trajectory file. Every Refresh rebuilds the file from the current DB state.
// This is the single export path — both --trajectory-json (runtime) and
// TUI export use the same TrajectoryFromMessages builder underneath.
type TrajectoryMaterializer struct {
	path     string
	sessions session.Service
	messages message.Service
	model    string
	extra    map[string]any
}

// NewTrajectoryMaterializer creates a materializer for the given output path.
// SetExtra can be used to inject state not in DB (e.g. interrupted flag).
func NewTrajectoryMaterializer(path string, sessions session.Service, messages message.Service, model string) *TrajectoryMaterializer {
	return &TrajectoryMaterializer{
		path:     path,
		sessions: sessions,
		messages: messages,
		model:    model,
	}
}

// SetExtra configures extra fields merged into the trajectory root extra on
// each Refresh. Call before Refresh to inject transient state like
// interrupted/reason.
func (m *TrajectoryMaterializer) SetExtra(extra map[string]any) {
	if m == nil {
		return
	}
	m.extra = extra
}

// Refresh reads the current session and messages from DB, builds the ATIF
// trajectory, and writes it to the output path atomically.
func (m *TrajectoryMaterializer) Refresh(ctx context.Context, sessionID string) error {
	if m == nil {
		return nil
	}
	var sess session.Session
	if m.sessions != nil {
		var err error
		sess, err = m.sessions.Get(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("trajectory materializer: get session: %w", err)
		}
	}
	var msgs []message.Message
	if m.messages != nil {
		var err error
		msgs, err = m.messages.List(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("trajectory materializer: list messages: %w", err)
		}
	}
	traj := TrajectoryFromMessages(sessionID, m.model, msgs, &sess)
	for k, v := range m.extra {
		if traj.Extra == nil {
			traj.Extra = map[string]any{}
		}
		traj.Extra[k] = v
	}
	return WriteTrajectoryFile(m.path, traj)
}
