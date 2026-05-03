package transcript

import (
	"context"
	"log/slog"
	"time"
)

// Recorder is the seam between the agent loop (Phase 1) and the .md transcript
// renderer (Phase 2). The agent loop calls these methods as events occur during
// a session; Phase 2 renders them to .md via MdRecorder.
//
// TrailerToken invariant: callers MUST hold the token returned by
// AgentEmit and pass it to exactly one of BashResult or BashSkipped.
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

	// AgentEmit writes a fenced code block for the model's emit (announce-then-classify pattern).
	// Returns a TrailerToken that must be passed to BashResult or BashSkipped.
	AgentEmit(ctx context.Context, sessionID, bash string) (TrailerToken, error)

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

// NoopRecorder implements Recorder with no-op methods. It is the default Recorder
// for Phase 1's agent loop in standalone tests where no .md output is needed.
type NoopRecorder struct{}

func (NoopRecorder) Open(_ context.Context, _ Meta) error             { return nil }
func (NoopRecorder) UserMessage(_ context.Context, _, _ string) error { return nil }
func (NoopRecorder) AgentEmit(_ context.Context, _, _ string) (TrailerToken, error) {
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

// MdRecorder is the concrete Recorder used by lenos main. It composes
// the formatter and MdWriter to append session events to a .md file.
type MdRecorder struct {
	writer *MdWriter
	now    func() time.Time // injectable clock for deterministic tests
}

// NewMdRecorder returns a configured MdRecorder for the given .md path.
func NewMdRecorder(path string) *MdRecorder {
	return &MdRecorder{
		writer: NewMdWriter(path),
		now:    time.Now,
	}
}

func (r *MdRecorder) Open(_ context.Context, meta Meta) error {
	return r.writer.Append([]byte(RenderFrontmatter(meta)))
}

func (r *MdRecorder) UserMessage(_ context.Context, _, text string) error {
	return r.writer.Append([]byte(RenderUserMessage(text)))
}

func (r *MdRecorder) AgentEmit(_ context.Context, sessionID, bash string) (TrailerToken, error) {
	startedAt := r.now()
	return TrailerToken{sessionID: sessionID, startedAt: startedAt}, r.writer.Append([]byte(RenderBashBlock(bash)))
}

func (r *MdRecorder) BashResult(_ context.Context, tok TrailerToken, out []byte, exitCode int, dur time.Duration) error {
	if exitCode == 0 {
		return r.writer.Append([]byte(RenderTrailerSuccess(tok.startedAt, dur)))
	}
	// Within-call atomic: output block + failure trailer written in one Append.
	// Note: if AgentBashAnnounce previously failed under E8 (write error,
	// bash block absent), the trailer appears with no preceding bash block —
	// this is an inter-call invariant we don't enforce.
	return r.writer.Append([]byte(RenderOutputBlock(out) + RenderTrailerFailure(tok.startedAt, dur, exitCode)))
}

func (r *MdRecorder) BashSkipped(_ context.Context, tok TrailerToken, sev Severity, desc string) error {
	// Per spec: bash block already written by AgentEmit.
	// BashSkipped writes the runtime-event blockquote; no output, no trailer.
	return r.writeRuntimeEvent(sev, desc)
}

func (r *MdRecorder) RuntimeEvent(_ context.Context, _ string, sev Severity, desc string) error {
	return r.writeRuntimeEvent(sev, desc)
}

func (r *MdRecorder) writeRuntimeEvent(sev Severity, desc string) error {
	return r.writer.Append([]byte(RenderRuntimeEvent(sev, desc)))
}

func (r *MdRecorder) TurnEnd(_ context.Context, _ string) error {
	return r.writer.Append([]byte(RenderTurnEnd()))
}

func (r *MdRecorder) Close() error {
	return nil
}

// Compile-time interface check.
var _ Recorder = (*MdRecorder)(nil)

// NewLoggingRecorder wraps a Recorder so the first error from any method is
// logged at Warn level; subsequent errors are silently dropped. This ensures a
// disk-full or permission error on .md writes surfaces at least once without
// halting the loop (per E8: recorder failures are non-halting).
func NewLoggingRecorder(r Recorder) Recorder {
	return &loggingRecorder{inner: r}
}

// loggingRecorder implements Recorder by delegating to an inner Recorder and
// logging the first error from each method at Warn level.
type loggingRecorder struct {
	inner  Recorder
	logged bool
}

func (r *loggingRecorder) logErr(method string, err error) {
	if !r.logged && err != nil {
		slog.Warn("transcript recorder: first failure (subsequent failures silenced)",
			"method", method, "error", err)
		r.logged = true
	}
}

func (r *loggingRecorder) Open(ctx context.Context, meta Meta) error {
	err := r.inner.Open(ctx, meta)
	r.logErr("Open", err)
	return err
}

func (r *loggingRecorder) UserMessage(ctx context.Context, sessionID, text string) error {
	err := r.inner.UserMessage(ctx, sessionID, text)
	r.logErr("UserMessage", err)
	return err
}

func (r *loggingRecorder) AgentEmit(ctx context.Context, sessionID, bash string) (TrailerToken, error) {
	tok, err := r.inner.AgentEmit(ctx, sessionID, bash)
	r.logErr("AgentEmit", err)
	return tok, err
}

func (r *loggingRecorder) BashResult(ctx context.Context, tok TrailerToken, out []byte, exitCode int, dur time.Duration) error {
	err := r.inner.BashResult(ctx, tok, out, exitCode, dur)
	r.logErr("BashResult", err)
	return err
}

func (r *loggingRecorder) BashSkipped(ctx context.Context, tok TrailerToken, sev Severity, desc string) error {
	err := r.inner.BashSkipped(ctx, tok, sev, desc)
	r.logErr("BashSkipped", err)
	return err
}

func (r *loggingRecorder) RuntimeEvent(ctx context.Context, sessionID string, sev Severity, desc string) error {
	err := r.inner.RuntimeEvent(ctx, sessionID, sev, desc)
	r.logErr("RuntimeEvent", err)
	return err
}

func (r *loggingRecorder) TurnEnd(ctx context.Context, sessionID string) error {
	err := r.inner.TurnEnd(ctx, sessionID)
	r.logErr("TurnEnd", err)
	return err
}

func (r *loggingRecorder) Close() error {
	err := r.inner.Close()
	r.logErr("Close", err)
	return err
}

var _ Recorder = (*loggingRecorder)(nil)
