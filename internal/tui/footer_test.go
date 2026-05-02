package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	uistyles "github.com/tta-lab/lenos/internal/ui/styles"
)

func bottomBarTestStyles() *uistyles.Styles {
	s := uistyles.DefaultStyles()
	return &s
}

func TestDeriveFooter(t *testing.T) {
	t.Run("file ending with only runtime-event blockquotes", func(t *testing.T) {
		md := []byte("---\n---\n\n> *runtime: ⚠️ blocked*\n> *runtime: still blocked*\n")
		d := DeriveFooter(md)
		assert.Equal(t, FooterStateIdle, d.State)
		assert.Equal(t, time.Duration(0), d.LastDuration)
	})

	t.Run("file ending with user msg (awaiting agent)", func(t *testing.T) {
		md := []byte("---\n---\n\n**λ** Open a PR\n")
		d := DeriveFooter(md)
		assert.Equal(t, FooterStateIdle, d.State)
	})

	t.Run("empty body", func(t *testing.T) {
		md := []byte("---\n---\n")
		d := DeriveFooter(md)
		assert.Equal(t, FooterStateIdle, d.State)
	})

	t.Run("active bash block", func(t *testing.T) {
		md := []byte("---\n---\n\n```bash\ngo test ./...\n```\n")
		d := DeriveFooter(md)
		assert.Equal(t, FooterStateActive, d.State)
		assert.Equal(t, "go test ./...", d.LatestBashCmd)
	})

	t.Run("bash with heredoc", func(t *testing.T) {
		md := []byte("---\n---\n\n```bash\nnarrate <<EOF\nsome text\nEOF\n```\n")
		d := DeriveFooter(md)
		assert.Equal(t, FooterStateActive, d.State)
		assert.Equal(t, "narrate <<EOF", d.LatestBashCmd)
	})

	t.Run("active lenos-bash block", func(t *testing.T) {
		md := []byte("---\n---\n\n```lenos-bash\ngo test ./...\n```\n")
		d := DeriveFooter(md)
		assert.Equal(t, FooterStateActive, d.State)
		assert.Equal(t, "go test ./...", d.LatestBashCmd)
	})

	t.Run("idle after trailer", func(t *testing.T) {
		md := []byte("---\n---\n\n*[10:30:05, 12s]*\n")
		d := DeriveFooter(md)
		assert.Equal(t, FooterStateIdle, d.State)
		assert.Equal(t, 12*time.Second, d.LastDuration)
	})

	t.Run("turn ended after trailer", func(t *testing.T) {
		md := []byte("---\n---\n\n*[10:30:05, 30s]*\n\n*(turn ended)*\n")
		d := DeriveFooter(md)
		assert.Equal(t, FooterStateTurnEnded, d.State)
		assert.Equal(t, 30*time.Second, d.LastDuration)
		assert.Equal(t, 1, d.TurnNumber)
	})
}

func TestFooterRender(t *testing.T) {
	styles := NewStyles()

	t.Run("active state shows elapsed time", func(t *testing.T) {
		f := NewFooter(styles)
		f.SetWidth(100)
		t0 := time.Now().Add(-47 * time.Second)
		f.Update(FooterDerivation{
			State:         FooterStateActive,
			LatestBashCmd: "go test ./...",
		}, t0)

		out := f.Render(time.Now(), time.Now())
		assert.Contains(t, out, "agent working")
		assert.Contains(t, out, "go test")
		assert.Contains(t, out, "running")
	})

	t.Run("idle state shows last cmd duration", func(t *testing.T) {
		f := NewFooter(styles)
		f.SetWidth(100)
		f.Update(FooterDerivation{
			State:        FooterStateIdle,
			LastDuration: 12 * time.Second,
		}, time.Time{})

		out := f.Render(time.Now(), time.Now())
		assert.Contains(t, out, "idle")
		assert.Contains(t, out, "12s")
	})

	t.Run("turn ended state", func(t *testing.T) {
		f := NewFooter(styles)
		f.SetWidth(100)
		f.Update(FooterDerivation{
			State:        FooterStateTurnEnded,
			LastDuration: 30 * time.Second,
			TurnNumber:   1,
		}, time.Time{})

		out := f.Render(time.Now(), time.Now())
		assert.Contains(t, out, "turn 1 ended")
		assert.Contains(t, out, "30s")
	})

	t.Run("very long bash command truncates", func(t *testing.T) {
		f := NewFooter(styles)
		f.SetWidth(60)
		f.Update(FooterDerivation{
			State:         FooterStateActive,
			LatestBashCmd: "this-is-a-very-long-command-that-definitely-exceeds-the-available-width-for-the-left-side",
		}, time.Now().Add(-5*time.Second))

		out := f.Render(time.Now(), time.Now())
		// Output should contain both the active state prefix and the hints.
		assert.Contains(t, out, "agent working")
		assert.Contains(t, out, "ctrl+c quit")
	})
}

func TestTick(t *testing.T) {
	cmd := Tick()
	msg := cmd()
	_, ok := msg.(TickMsg)
	assert.True(t, ok, "Tick should return a TickMsg")
}

func TestBottomBar_HiddenWhenQueueZero(t *testing.T) {
	b := NewBottomBar(NewStyles(), bottomBarTestStyles())
	b.SetWidth(80)
	b.SetQueue(0, nil)
	assert.Equal(t, "", b.Render(), "empty queue renders nothing")
}

func TestBottomBar_CompactRendersNQueued(t *testing.T) {
	b := NewBottomBar(NewStyles(), bottomBarTestStyles())
	b.SetWidth(80)
	b.SetQueue(3, []string{"a", "b", "c"})
	out := b.Render()
	assert.Contains(t, out, "3 Queued", "compact row shows count")
	assert.Contains(t, out, "ctrl+t", "compact row shows toggle hint")
	// The bordered pill itself is 3 visual lines; what matters is that the
	// expanded list rows aren't appended in compact mode.
	assert.NotContains(t, out, "a\n", "compact row does not include queue items")
	assert.NotContains(t, out, "b\n", "compact row does not include queue items")
}

func TestBottomBar_ExpandedRendersFullList(t *testing.T) {
	b := NewBottomBar(NewStyles(), bottomBarTestStyles())
	b.SetWidth(80)
	b.SetQueue(2, []string{"first", "second"})
	b.Toggle()
	assert.False(t, b.IsCompact())

	out := b.Render()
	assert.Contains(t, out, "2 Queued", "expanded still shows count")
	assert.Contains(t, out, "first", "expanded shows queue item 1")
	assert.Contains(t, out, "second", "expanded shows queue item 2")
	assert.GreaterOrEqual(t, strings.Count(out, "\n"), 2, "expanded spans multiple rows")
}

func TestBottomBar_ToggleTransitionRoundTrip(t *testing.T) {
	b := NewBottomBar(NewStyles(), bottomBarTestStyles())
	b.SetWidth(80)
	b.SetQueue(2, []string{"x", "y"})

	compact := b.Render()
	assert.True(t, b.IsCompact())

	b.Toggle()
	expanded := b.Render()
	assert.False(t, b.IsCompact())
	assert.NotEqual(t, compact, expanded, "expanded differs from compact")

	b.Toggle()
	assert.True(t, b.IsCompact())
	assert.Equal(t, compact, b.Render(), "compact restored after round trip")
}

func TestParseTrailerDuration(t *testing.T) {
	tests := []struct {
		line string
		dur  time.Duration
	}{
		{"*[10:30:05, 12s]*", 12 * time.Second},
		{"*[10:30:05, 0.120s]*", 120 * time.Millisecond},
		{"*[10:30:05, 1s]*", 1 * time.Second},
		{"*[10:30:05, 60s]*", 60 * time.Second},
	}

	for _, tt := range tests {
		t.Run(strings.ReplaceAll(tt.line, ":", "-"), func(t *testing.T) {
			d := parseTrailerDuration(tt.line)
			assert.Equal(t, tt.dur, d)
		})
	}
}
