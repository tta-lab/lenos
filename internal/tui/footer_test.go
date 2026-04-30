package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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
