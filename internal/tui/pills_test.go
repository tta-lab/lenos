package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tta-lab/lenos/internal/session"
	uistyles "github.com/tta-lab/lenos/internal/ui/styles"
)

func pillTestStyles() *uistyles.Styles {
	s := uistyles.DefaultStyles()
	return &s
}

func TestQueuePill_ZeroReturnsEmpty(t *testing.T) {
	out := queuePill(0, false, false, pillTestStyles())
	assert.Empty(t, out, "queuePill returns empty string when count is 0")
}

func TestQueuePill_WithCountRendersTrianglesAndText(t *testing.T) {
	out := queuePill(3, true, true, pillTestStyles())
	assert.Contains(t, out, "▶", "rendered pill includes triangle glyph(s)")
	assert.Contains(t, out, "3 Queued", "rendered pill includes count and label")
}

func TestQueueList_TruncatesLongItems(t *testing.T) {
	long := strings.Repeat("x", maxQueueDisplayLength+10)
	out := queueList([]string{long}, pillTestStyles())
	assert.Contains(t, out, "…", "long queue items get an ellipsis")
	// Truncated visible payload must not contain the original full string.
	assert.NotContains(t, out, long, "long item is not rendered in full")
}

func TestFormatTodosList_SortsByStatus(t *testing.T) {
	todos := []session.Todo{
		{Content: "pending-one", Status: session.TodoStatusPending},
		{Content: "completed-one", Status: session.TodoStatusCompleted},
		{Content: "in-progress-one", Status: session.TodoStatusInProgress},
	}
	out := FormatTodosList(pillTestStyles(), todos, "→", 80)
	cIdx := strings.Index(out, "completed-one")
	pIdx := strings.Index(out, "in-progress-one")
	xIdx := strings.Index(out, "pending-one")
	assert.NotEqual(t, -1, cIdx)
	assert.NotEqual(t, -1, pIdx)
	assert.NotEqual(t, -1, xIdx)
	assert.Less(t, cIdx, pIdx, "completed sorts before in_progress")
	assert.Less(t, pIdx, xIdx, "in_progress sorts before pending")
}

func TestTodoPill_RendersProgressWithActiveForm(t *testing.T) {
	todos := []session.Todo{
		{Content: "Write the doc", ActiveForm: "Writing the doc", Status: session.TodoStatusInProgress},
		{Content: "Done thing", Status: session.TodoStatusCompleted},
		{Content: "Pending thing", Status: session.TodoStatusPending},
	}
	// panelFocused=false so the active-form branch is exercised.
	out := todoPill(todos, "→", false, false, pillTestStyles())
	assert.Contains(t, out, "To-Do")
	assert.Contains(t, out, "1/3", "progress reflects 1 completed of 3")
	assert.Contains(t, out, "Writing the doc", "active form is preferred over Content")
}
