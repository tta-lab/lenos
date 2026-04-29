package transcript

import (
	"context"
	"time"
)

// Recorder is the seam between the agent loop (Phase 1) and the .md transcript
// renderer (Phase 2). The agent loop calls these methods as events occur during
// a session; Phase 2 renders them to .md via MdRecorder.
//
// TrailerToken invariant: callers MUST hold the token returned by
// AgentBashAnnounce and pass it to exactly one of BashResult or BashSkipped.
// Passing the same token to both is undefined behavior.
//
// Methods must not panic; implementation should only log errors on write
// failures (per E8: .md write failure is non-halting).
type Recorder interface {
	// Open writes the YAML frontmatter and initialises the transcript.
	// Must be called exactly once per session, before any other method.
	Open(ctx context.Context, meta Meta) error

	// UserMessage writes a **λ <text>** line (spec 57a09f51 §Format).
	UserMessage(ctx context.Context, sessionID, text string) error

	// AgentBashAnnounce writes a fenced bash block (announce-then-run pattern).
	// Returns a TrailerToken that must be passed to BashResult or BashSkipped.
	AgentBashAnnounce(ctx context.Context, sessionID, bash string) (TrailerToken, error)

	// BashResult writes the output block and trailer for a completed command.
	// On exit==0: trailer only (success-quiet). On exit!=0: output block +
	// failure trailer with ❌ exit N (spec 57a09f51 §Error Visibility).
	BashResult(ctx context.Context, tok TrailerToken, out []byte, exitCode int, dur time.Duration) error

	// BashSkipped writes a runtime-event blockquote tied to the bash unit
	// identified by tok. No output block, no trailer is written (per spec
	// 57a09f51 §Composability with runtime events).
	BashSkipped(ctx context.Context, tok TrailerToken, sev Severity, description string) error

	// RuntimeEvent writes a standalone runtime-event blockquote not tied to
	// a specific bash command (e.g. step-cap hit, provider error).
	RuntimeEvent(ctx context.Context, sessionID string, sev Severity, description string) error

	// TurnEnd writes the *(turn ended)* italic line (spec 57a09f51 §Format).
	TurnEnd(ctx context.Context, sessionID string) error

	// Close finalises the transcript (currently a no-op since MdWriter uses
	// per-call open/close). May become meaningful on future resource types.
	Close() error
}

// NoopRecorder is the default Recorder that Phase 1's agent loop uses in
// standalone tests where no .md output is needed. All methods return zero
// values and nil errors.
type NoopRecorder struct{}

func (NoopRecorder) Open(_ context.Context, _ Meta) error             { return nil }
func (NoopRecorder) UserMessage(_ context.Context, _, _ string) error { return nil }
func (NoopRecorder) AgentBashAnnounce(_ context.Context, _, _ string) (TrailerToken, error) {
	return TrailerToken{}, nil
}

func (NoopRecorder) BashResult(_ context.Context, _ TrailerToken, _ []byte, _ int, _ time.Duration) error {
	return nil
}

func (NoopRecorder) BashSkipped(_ context.Context, _ TrailerToken, _ Severity, _ string) error {
	return nil
}

func (NoopRecorder) RuntimeEvent(_ context.Context, _ string, _ Severity, _ string) error {
	return nil
}
func (NoopRecorder) TurnEnd(_ context.Context, _ string) error { return nil }
func (NoopRecorder) Close() error                              { return nil }

// Compile-time interface checks.
var (
	_ Recorder = (*NoopRecorder)(nil)
)
