package model

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

// formatTodoSegment is the building block for the compact header's `TODO x/N`
// strip. Empty inputs must collapse silently; populated inputs must report
// completed-vs-total.
func TestFormatTodoSegment(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()

	t.Run("empty todos render no segment", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, formatTodoSegment(&sty, nil))
		assert.Empty(t, formatTodoSegment(&sty, []session.Todo{}))
	})

	t.Run("counts completed vs total", func(t *testing.T) {
		t.Parallel()
		todos := []session.Todo{
			{Status: session.TodoStatusCompleted},
			{Status: session.TodoStatusCompleted},
			{Status: session.TodoStatusInProgress},
			{Status: session.TodoStatusPending},
		}
		got := ansi.Strip(formatTodoSegment(&sty, todos))
		assert.True(t, strings.HasPrefix(got, "TODO"), "label first: %q", got)
		assert.Contains(t, got, "2/4", "completed/total format")
	})

	t.Run("no completed shows 0/N", func(t *testing.T) {
		t.Parallel()
		todos := []session.Todo{
			{Status: session.TodoStatusPending},
			{Status: session.TodoStatusInProgress},
		}
		got := ansi.Strip(formatTodoSegment(&sty, todos))
		assert.Contains(t, got, "0/2")
	})
}
