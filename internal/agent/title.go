package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/tta-lab/lenos/internal/config"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// titleManager writes OSC 0 terminal title updates with a spinner during
// agent activity and optionally renames the tmux window for single-pane
// sessions. Writes are skipped when stdout is not a terminal.
type titleManager struct {
	mu        sync.Mutex
	refcount  int32
	stopCh    chan struct{}
	agentName string
	tty       *os.File
	done      chan struct{}
	started   bool
}

func newTitleManager(agentName string) *titleManager {
	tm := &titleManager{
		agentName: agentName,
	}
	if term.IsTerminal(os.Stdout.Fd()) {
		tm.tty = os.Stdout
	}
	return tm
}

// StartWorking increments the work refcount. If this is the first active
// reference it starts the spinner goroutine.
func (t *titleManager) StartWorking() {
	if t.tty == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if atomic.AddInt32(&t.refcount, 1) == 1 && !t.started {
		t.started = true
		t.stopCh = make(chan struct{})
		t.done = make(chan struct{})
		go t.spin()
	}
}

// StopWorking decrements the work refcount. When the refcount reaches zero
// the spinner stops and the title resets to the idle state.
func (t *titleManager) StopWorking() {
	if t.tty == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if atomic.AddInt32(&t.refcount, -1) == 0 && t.started {
		close(t.stopCh)
		<-t.done
		t.started = false
	}
}

func (t *titleManager) spin() {
	defer close(t.done)
	t.writeTitle(fmt.Sprintf("Lenos %s", spinnerFrames[0]))
	frameIdx := 0
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			t.writeTitle("Lenos")
			return
		case <-ticker.C:
			frameIdx = (frameIdx + 1) % len(spinnerFrames)
			t.writeTitle(fmt.Sprintf("Lenos %s", spinnerFrames[frameIdx]))
		}
	}
}

func (t *titleManager) writeTitle(title string) {
	sanitized := sanitizeTitle(title)
	fmt.Fprintf(t.tty, "\x1b]0;%s\x07", sanitized)
}

// sanitizeTitle strips control characters and collapses whitespace.
func sanitizeTitle(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 {
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// tmuxRenameWindow checks whether the process is inside tmux with a single
// pane and renames the window to the agent name. Failures are silent
// (best-effort).
func tmuxRenameWindow(agentName string) {
	if os.Getenv("TMUX") == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "#{window_panes}").Output()
	if err != nil {
		return
	}
	if strings.TrimSpace(string(out)) != "1" {
		return
	}
	_ = exec.CommandContext(ctx, "tmux", "rename-window", agentName).Run()
}

// shouldEnableTitle returns true for native coder and reviewer agents.
func shouldEnableTitle(store *config.ConfigStore) bool {
	switch store.Overrides().AgentName {
	case config.AgentCoder, config.AgentReviewer:
		return true
	default:
		return false
	}
}
