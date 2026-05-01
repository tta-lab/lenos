package tui

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tta-lab/lenos/internal/agent/notify"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/pubsub"
	"github.com/tta-lab/lenos/internal/ui/notification"
)

// recordingBackend captures every Send call for assertion in tests.
type recordingBackend struct {
	mu      sync.Mutex
	sent    []notification.Notification
	sendErr error
}

func (r *recordingBackend) Send(n notification.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, n)
	return r.sendErr
}

func (r *recordingBackend) calls() []notification.Notification {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]notification.Notification, len(r.sent))
	copy(out, r.sent)
	return out
}

func newDispatcherWithRecorder(cfg *config.Config) (*NotificationDispatcher, *recordingBackend) {
	rec := &recordingBackend{}
	d := NewNotificationDispatcher(cfg)
	d.SetBackend(rec)
	return d, rec
}

func agentFinishedEvent() pubsub.Event[notify.Notification] {
	return pubsub.Event[notify.Notification]{
		Type: pubsub.UpdatedEvent,
		Payload: notify.Notification{
			SessionID:    "sess-1",
			SessionTitle: "ship the feature",
			Type:         notify.TypeAgentFinished,
		},
	}
}

func TestDispatch_SkippedWhenFocused(t *testing.T) {
	d, rec := newDispatcherWithRecorder(&config.Config{Options: &config.Options{}})
	d.SetFocused(true)
	d.HandleEvent(agentFinishedEvent())
	assert.Empty(t, rec.calls(), "focused window suppresses notification")
}

func TestDispatch_SkippedWhenDisabled(t *testing.T) {
	cfg := &config.Config{Options: &config.Options{DisableNotifications: true}}
	d, rec := newDispatcherWithRecorder(cfg)
	d.SetFocused(false)
	d.HandleEvent(agentFinishedEvent())
	assert.Empty(t, rec.calls(), "disable_notifications=true suppresses delivery")
}

func TestDispatch_SkippedWhenWrongType(t *testing.T) {
	d, rec := newDispatcherWithRecorder(&config.Config{Options: &config.Options{}})
	d.SetFocused(false)

	evt := pubsub.Event[notify.Notification]{
		Payload: notify.Notification{
			SessionID:    "sess-1",
			SessionTitle: "ship the feature",
			Type:         notify.Type("unknown"),
		},
	}
	d.HandleEvent(evt)
	assert.Empty(t, rec.calls(), "non-AgentFinished events are ignored")
}

func TestDispatch_FiresWhenUnfocusedAndEnabled(t *testing.T) {
	d, rec := newDispatcherWithRecorder(&config.Config{Options: &config.Options{}})
	d.SetFocused(false)
	d.HandleEvent(agentFinishedEvent())

	calls := rec.calls()
	assert.Len(t, calls, 1, "unfocused + enabled + AgentFinished → 1 send")
	assert.Contains(t, calls[0].Title, "Lenos")
	assert.Contains(t, calls[0].Message, "ship the feature")
}
