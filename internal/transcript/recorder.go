package transcript

import (
	"context"
	"time"
)

type Severity int

const (
	SevNormal Severity = iota
	SevWarn
	SevError
)

type TrailerToken struct{}

type Meta struct {
	SessionID string
	Agent     string
	Model     string
	StartedAt time.Time
	Sandbox   string
	Title     string
	Cwd       string
}

type Recorder interface {
	Open(context.Context, Meta) error
	UserMessage(ctx context.Context, sessionID, text string) error
	AgentEmit(ctx context.Context, sessionID, emit string) (TrailerToken, error)
	ProseMessage(ctx context.Context, sessionID, text string) error
	BashResult(ctx context.Context, _ TrailerToken, out []byte, exitCode int, _ time.Duration) error
	BashSkipped(ctx context.Context, _ TrailerToken, sev Severity, desc string) error
	RuntimeEvent(ctx context.Context, sessionID string, sev Severity, desc string) error
	TurnEnd(ctx context.Context, sessionID string) error
	Close() error
}

type NoopRecorder struct{}

func (NoopRecorder) Open(context.Context, Meta) error                  { return nil }
func (NoopRecorder) UserMessage(context.Context, string, string) error { return nil }
func (NoopRecorder) AgentEmit(context.Context, string, string) (TrailerToken, error) {
	return TrailerToken{}, nil
}
func (NoopRecorder) ProseMessage(context.Context, string, string) error { return nil }
func (NoopRecorder) BashResult(context.Context, TrailerToken, []byte, int, time.Duration) error {
	return nil
}
func (NoopRecorder) BashSkipped(context.Context, TrailerToken, Severity, string) error { return nil }
func (NoopRecorder) RuntimeEvent(context.Context, string, Severity, string) error      { return nil }
func (NoopRecorder) TurnEnd(context.Context, string) error                             { return nil }
func (NoopRecorder) Close() error                                                      { return nil }

func NewLoggingRecorder(inner Recorder) Recorder { return inner }
func NewMdRecorder(path string) Recorder         { return NoopRecorder{} }

// Watcher stubs
type Watcher struct{}

func NewWatcher(_ string) *Watcher       { return &Watcher{} }
func (w *Watcher) Start(context.Context) {}
func (w *Watcher) Close() error          { return nil }

const (
	WatchAppend   = "append"
	WatchTruncate = "truncate"
	WatchError    = "error"
)

type WatchEvent struct {
	Bytes []byte
	Err   error
}
