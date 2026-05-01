package tui

import (
	"fmt"
	"log/slog"

	"github.com/tta-lab/lenos/internal/agent/notify"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/pubsub"
	"github.com/tta-lab/lenos/internal/ui/notification"
)

// NotificationDispatcher gates desktop notification delivery on focus state
// and config policy. It wraps a notification.Backend so tests can substitute a
// no-op or recording backend.
type NotificationDispatcher struct {
	backend notification.Backend
	cfg     *config.Config
	focused bool
}

// NewNotificationDispatcher constructs a dispatcher backed by the native
// platform notifier (beeep via NativeBackend). Cross-platform — the icon
// payload is selected at compile time via build tags in
// internal/ui/notification.
func NewNotificationDispatcher(cfg *config.Config) *NotificationDispatcher {
	return &NotificationDispatcher{
		backend: notification.NewNativeBackend(notification.Icon),
		cfg:     cfg,
		focused: true, // default focused — wait for an explicit BlurMsg
	}
}

// SetFocused records the latest window-focus state.
func (d *NotificationDispatcher) SetFocused(focused bool) {
	d.focused = focused
}

// SetBackend swaps the backend; primarily used by tests.
func (d *NotificationDispatcher) SetBackend(b notification.Backend) {
	d.backend = b
}

// HandleEvent dispatches a domain notification through the backend if the
// dispatch policy allows it: window unfocused, notifications enabled in
// config, payload is TypeAgentFinished. Anything else returns without sending.
func (d *NotificationDispatcher) HandleEvent(evt pubsub.Event[notify.Notification]) {
	if d == nil || d.backend == nil {
		return
	}
	if d.focused {
		return
	}
	if d.cfg != nil && d.cfg.Options != nil && d.cfg.Options.DisableNotifications {
		return
	}
	if evt.Payload.Type != notify.TypeAgentFinished {
		return
	}

	n := notification.Notification{
		Title:   "Lenos is waiting...",
		Message: fmt.Sprintf("Agent's turn completed in %q", evt.Payload.SessionTitle),
	}
	if err := d.backend.Send(n); err != nil {
		slog.Warn("failed to dispatch desktop notification", "err", err)
	}
}
