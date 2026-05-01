package tui

import (
	"context"
	"log/slog"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/tta-lab/lenos/internal/ui/common"
)

// MinTextareaHeight / MaxTextareaHeight bound the input editor height.
const (
	MinTextareaHeight = 1
	MaxTextareaHeight = 12
)

// inputPane is the bash-first input editor. Step 11 lands a minimal core:
// multiline textarea + Enter→Submit→AgentRun. Dialogs (commands / sessions /
// models / filepicker), completions (slash + @-mention), attachments, paste,
// and history are reserved for Step 11.b — the legacy packages
// (internal/ui/dialog, internal/ui/completions, internal/ui/attachments,
// charm.land/bubbles/v2/textarea) stay intact as a library so the wire-up
// is additive.
//
// INVARIANT: Submit MUST NOT call recorder.UserMessage. The agent
// coordinator already writes the user-message **λ** line at agent_run.go
// (cacdab93 / PR #51 H6). Calling it here would double-fire the line.
type inputPane struct {
	com       *common.Common
	sessionID string

	textarea textarea.Model
	width    int
}

// newInputPane constructs an empty stub-mode input pane. Wire it via
// Configure once the App has resolved the session ID and Common.
func newInputPane() *inputPane {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.CharLimit = -1
	ta.SetVirtualCursor(false)
	ta.DynamicHeight = true
	ta.MinHeight = MinTextareaHeight
	ta.MaxHeight = MaxTextareaHeight
	ta.Focus()

	return &inputPane{textarea: ta}
}

// Configure attaches the workspace context. Called by App.New after session
// resolution so the input pane can submit via AgentRun.
func (p *inputPane) Configure(com *common.Common, sessionID string) {
	if p == nil {
		return
	}
	p.com = com
	p.sessionID = sessionID
}

// Focus / Blur expose the underlying textarea focus state.
func (p *inputPane) Focus() tea.Cmd {
	if p == nil {
		return nil
	}
	return p.textarea.Focus()
}

func (p *inputPane) Blur() {
	if p == nil {
		return
	}
	p.textarea.Blur()
}

// IsFocused reports whether the textarea currently owns the cursor.
func (p *inputPane) IsFocused() bool {
	if p == nil {
		return false
	}
	return p.textarea.Focused()
}

// SetWidth propagates the terminal width down to the textarea.
func (p *inputPane) SetWidth(w int) {
	if p == nil {
		return
	}
	p.width = w
	p.textarea.SetWidth(w)
}

// Update handles input keys: Enter submits, anything else routes to the
// textarea. Returns the issued command (typically nil) so the App's Update
// can chain it.
func (p *inputPane) Update(msg tea.Msg) tea.Cmd {
	if p == nil {
		return nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		// Enter (without Shift) submits.
		if isEnter(km) {
			return p.Submit()
		}
	}
	var cmd tea.Cmd
	p.textarea, cmd = p.textarea.Update(msg)
	return cmd
}

// Submit pulls the current textarea contents, resets the editor, and
// dispatches the prompt to AgentRun. Must NOT touch the recorder — see the
// type-level invariant.
func (p *inputPane) Submit() tea.Cmd {
	if p == nil || p.com == nil || p.com.Workspace == nil {
		return nil
	}
	prompt := strings.TrimRight(p.textarea.Value(), "\n")
	if prompt == "" {
		return nil
	}
	p.textarea.Reset()

	ws := p.com.Workspace
	sessionID := p.sessionID
	return func() tea.Msg {
		if err := ws.AgentRun(context.Background(), sessionID, prompt); err != nil {
			slog.Warn("AgentRun failed from inputPane", "err", err)
		}
		return nil
	}
}

// Render renders the textarea. Returns "" until the App wires it up — the
// app composition fall back when this returns empty.
func (p *inputPane) Render() string {
	if p == nil {
		return ""
	}
	return p.textarea.View()
}

func isEnter(km tea.KeyMsg) bool {
	if km.String() == "enter" {
		return true
	}
	// Match via key bindings if/when the App passes a binding; for now the
	// raw string check is enough for the textarea API.
	_ = key.Binding{}
	return false
}
