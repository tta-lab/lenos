package tui

import (
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/stretchr/testify/assert"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/workspace"
)

// mockWorkspace implements SandboxProvider for testing.
type mockWorkspace struct {
	sandboxState string
	model        workspace.AgentModel
}

func (m *mockWorkspace) AgentSandboxState() string        { return m.sandboxState }
func (m *mockWorkspace) AgentModel() workspace.AgentModel { return m.model }
func (m *mockWorkspace) AgentName() string                { return "test-agent" }

func newTestFM() Frontmatter {
	return Frontmatter{
		Agent:     "kestrel",
		SessionID: "7d3e8a91-abcd-efgh-ijkl-mnopqrstuvwx",
		Model:     "claude-sonnet-4-6",
		Title:     "TUI redesign",
	}
}

func TestHeader_CompactDefault(t *testing.T) {
	h := NewHeader(nil, newTestFM(), NewStyles())
	assert.True(t, h.IsCompact(), "new Header is compact by default")
}

func TestHeader_CompactWithFullData(t *testing.T) {
	mw := &mockWorkspace{
		sandboxState: "on",
		model: workspace.AgentModel{
			CatwalkCfg: catwalk.Model{ContextWindow: 200000},
		},
	}
	h := NewHeader(mw, newTestFM(), NewStyles())
	h.SetWidth(120)
	h.SetTodos([]session.Todo{
		{Content: "task1", Status: session.TodoStatusCompleted},
		{Content: "task2", Status: session.TodoStatusPending},
		{Content: "task3", Status: session.TodoStatusPending},
	})
	h.SetSession(&session.Session{PromptTokens: 50000, CompletionTokens: 34000})

	out := h.Render()
	assert.Contains(t, out, "Lenos")
	assert.Contains(t, out, "kestrel")
	assert.Contains(t, out, "[on]")
	assert.Contains(t, out, "42%")
	assert.Contains(t, out, "TODO 1/3")
	assert.Contains(t, out, "ctrl+d")
}

func TestHeader_CompactTodoHiddenWhenEmpty(t *testing.T) {
	h := NewHeader(nil, newTestFM(), NewStyles())
	h.SetWidth(120)
	out := h.Render()
	assert.NotContains(t, out, "TODO")
}

func TestHeader_ExpandedRender(t *testing.T) {
	mw := &mockWorkspace{
		sandboxState: "on",
		model:        workspace.AgentModel{CatwalkCfg: catwalk.Model{ContextWindow: 200000}},
	}
	h := NewHeader(mw, newTestFM(), NewStyles())
	h.SetWidth(120)
	h.SetTodos([]session.Todo{
		{Content: "ship it", Status: session.TodoStatusPending},
	})
	h.SetGitFiles([]workspace.ModifiedFile{
		{Path: "a.go"},
		{Path: "b.go"},
	})
	h.Toggle()
	assert.False(t, h.IsCompact())

	out := h.Render()
	assert.Contains(t, out, "TUI redesign", "title row present")
	assert.Contains(t, out, "2 modified file", "git files row present")
	assert.Contains(t, out, "sandbox active", "sandbox detail row present")
	assert.Contains(t, out, "[ ] ship it", "todo list row present")
	assert.GreaterOrEqual(t, strings.Count(out, "\n"), 4, "expanded render is multi-row")
}

func TestHeader_ToggleTransitionRoundTrip(t *testing.T) {
	h := NewHeader(nil, newTestFM(), NewStyles())
	h.SetWidth(120)

	compact := h.Render()
	assert.True(t, h.IsCompact())

	h.Toggle()
	assert.False(t, h.IsCompact())
	expanded := h.Render()
	assert.NotEqual(t, compact, expanded, "expanded must differ from compact")

	h.Toggle()
	assert.True(t, h.IsCompact())
	assert.Equal(t, compact, h.Render(), "compact render restored after round trip")
}

func TestHeader_SandboxThreeStateStyles(t *testing.T) {
	render := func(state string, com SandboxProvider) string {
		h := NewHeader(com, newTestFM(), NewStyles())
		h.SetWidth(120)
		_ = state
		return h.Render()
	}

	mwOn := &mockWorkspace{sandboxState: "on"}
	mwOff := &mockWorkspace{sandboxState: "off"}

	on := render("on", mwOn)
	off := render("off", mwOff)
	deg := render("degraded", nil) // nil com → degraded fallback

	assert.Contains(t, on, "[on]")
	assert.Contains(t, off, "[off]")
	assert.Contains(t, deg, "[degraded]")

	// Distinct ANSI escapes per state — palette divergence is what users see.
	onTok := extractStyledToken(t, on, "[on]")
	offTok := extractStyledToken(t, off, "[off]")
	degTok := extractStyledToken(t, deg, "[degraded]")
	assert.NotEqual(t, onTok, offTok, "on and off render with distinct ANSI")
	assert.NotEqual(t, onTok, degTok, "on and degraded render with distinct ANSI")
	assert.NotEqual(t, offTok, degTok, "off and degraded render with distinct ANSI")
}

func TestHeader_GitFilesCountInExpanded(t *testing.T) {
	h := NewHeader(nil, newTestFM(), NewStyles())
	h.SetWidth(120)
	h.SetGitFiles([]workspace.ModifiedFile{
		{Path: "a.go"},
		{Path: "b.go"},
		{Path: "c.go"},
	})
	h.Toggle()
	out := h.Render()
	assert.Contains(t, out, "3 modified file")
}

func TestHeader_SetSessionUpdatesCtxPct(t *testing.T) {
	mw := &mockWorkspace{
		sandboxState: "on",
		model:        workspace.AgentModel{CatwalkCfg: catwalk.Model{ContextWindow: 100000}},
	}
	h := NewHeader(mw, newTestFM(), NewStyles())
	h.SetWidth(120)

	h.SetSession(&session.Session{PromptTokens: 5000, CompletionTokens: 5000})
	assert.Contains(t, h.Render(), "10%")

	// Updating the session changes the percentage on the next render.
	h.SetSession(&session.Session{PromptTokens: 30000, CompletionTokens: 30000})
	out := h.Render()
	assert.Contains(t, out, "60%")
	assert.NotContains(t, out, "10%")
}

// extractStyledToken finds the substring in s that ends with the given visible
// token and includes its leading ANSI SGR escape. Used to compare per-state
// styling without coupling tests to specific color codes.
func extractStyledToken(t *testing.T, s, visible string) string {
	t.Helper()
	idx := strings.Index(s, visible)
	if idx < 0 {
		t.Fatalf("visible token %q not found in %q", visible, s)
	}
	// Walk backwards to the most recent ESC byte preceding the token.
	start := strings.LastIndexByte(s[:idx], 0x1b)
	if start < 0 {
		// No escape preceding — return the visible token itself.
		return visible
	}
	end := idx + len(visible)
	// Include the trailing reset escape if present, to capture the closing SGR.
	tail := s[end:]
	if rIdx := strings.Index(tail, "\x1b[0m"); rIdx >= 0 && rIdx < 8 {
		end += rIdx + len("\x1b[0m")
	}
	return s[start:end]
}
