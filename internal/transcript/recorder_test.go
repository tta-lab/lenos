package transcript

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeClock returns successive times from a slice on each call. Used to make
// AgentEmit's startedAt deterministic across a multi-event sequence.
func fakeClock(t *testing.T, times []time.Time) func() time.Time {
	t.Helper()
	i := 0
	return func() time.Time {
		require.Less(t, i, len(times), "clock called more times than expected")
		v := times[i]
		i++
		return v
	}
}

func TestMdRecorder_FullSession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "session.md")

	base := time.Date(2026, 4, 28, 10, 30, 0, 0, time.UTC)
	r := NewMdRecorder(path)
	r.now = fakeClock(t, []time.Time{
		base.Add(5 * time.Second),  // announce 1 — go test fail
		base.Add(30 * time.Second), // announce 2 — narrate diagnosis
		base.Add(31 * time.Second), // announce 3 — sed -i banned
		base.Add(31 * time.Second), // announce 4 — narrate switching
		base.Add(45 * time.Second), // announce 5 — src edit
		base.Add(60 * time.Second), // announce 6 — go test success
	})

	ctx := context.Background()
	const sid = "7d3e8a91-abcd-efgh-ijkl-mnopqrstuvwx"

	require.NoError(t, r.Open(ctx, Meta{
		SessionID: sid,
		Agent:     "kestrel",
		Model:     "claude-sonnet-4-6",
		StartedAt: base,
	}))
	require.NoError(t, r.UserMessage(ctx, sid, "Find the auth bug in src/auth.go"))

	tok1, err := r.AgentEmit(ctx, sid, "go test ./auth")
	require.NoError(t, err)
	require.NoError(t, r.BashResult(ctx, tok1,
		[]byte("expected: 2026-01-01\ngot:      2025-12-31\nFAIL TestAuthExpiry"),
		1, 120*time.Millisecond))

	tok2, err := r.AgentEmit(ctx, sid,
		"narrate <<EOF\nexpiry comparison is reversed — t.ExpiresAt.Before(time.Now()) should be After\nEOF")
	require.NoError(t, err)
	// narrate prose written by cmd/narrate directly via RenderProse — simulate it.
	require.NoError(t, r.writer.Append([]byte(RenderProse(
		"expiry comparison is reversed — t.ExpiresAt.Before(time.Now()) should be After"))))
	require.NoError(t, r.BashResult(ctx, tok2, nil, 0, 1*time.Millisecond))

	tok3, err := r.AgentEmit(ctx, sid, "sed -i 's/Before/After/' src/auth.go")
	require.NoError(t, err)
	require.NoError(t, r.BashSkipped(ctx, tok3, SevWarn, "blocked: sed -i not allowed; use src edit"))

	tok4, err := r.AgentEmit(ctx, sid, `narrate "switching approach — using src edit"`)
	require.NoError(t, err)
	require.NoError(t, r.writer.Append([]byte(RenderProse("switching approach — using src edit"))))
	require.NoError(t, r.BashResult(ctx, tok4, nil, 0, 1*time.Millisecond))

	tok5, err := r.AgentEmit(ctx, sid, "src edit src/auth.go <<EOF\n... edit ...\nEOF")
	require.NoError(t, err)
	require.NoError(t, r.BashResult(ctx, tok5, nil, 0, 1200*time.Millisecond))

	tok6, err := r.AgentEmit(ctx, sid, "go test ./auth")
	require.NoError(t, err)
	require.NoError(t, r.BashResult(ctx, tok6, nil, 0, 30*time.Second))

	require.NoError(t, r.TurnEnd(ctx, sid))
	require.NoError(t, r.UserMessage(ctx, sid, "Open a PR"))
	require.NoError(t, r.Close())

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	want, err := os.ReadFile("testdata/full_session.md")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func TestMdRecorder_BashSkipped_AllSeverities(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		sev        Severity
		desc       string
		goldenFile string
	}{
		{"normal", SevNormal, "step cap hit", "testdata/recorder_bash_skipped_normal.md"},
		{"warn", SevWarn, "step cap hit", "testdata/recorder_bash_skipped_warn.md"},
		{"error", SevError, "provider error", "testdata/recorder_bash_skipped_error.md"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "session.md")

			base := time.Date(2026, 4, 28, 10, 30, 0, 0, time.UTC)
			r := NewMdRecorder(path)
			r.now = fakeClock(t, []time.Time{base})

			ctx := context.Background()
			const sid = "test-session"

			require.NoError(t, r.Open(ctx, Meta{
				SessionID: sid,
				Agent:     "test",
				Model:     "test-model",
				StartedAt: base,
			}))
			require.NoError(t, r.UserMessage(ctx, sid, "test"))

			tok, err := r.AgentEmit(ctx, sid, "some command")
			require.NoError(t, err)
			require.NoError(t, r.BashSkipped(ctx, tok, tc.sev, tc.desc))
			require.NoError(t, r.TurnEnd(ctx, sid))

			got, err := os.ReadFile(path)
			require.NoError(t, err)
			want, err := os.ReadFile(tc.goldenFile)
			require.NoError(t, err)
			require.Equal(t, string(want), string(got),
				"composability rule: bash block + runtime-event blockquote, NO output, NO trailer")
		})
	}
}

func TestE8MdRecorderWriteFailureNonHalting(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "readonly.md")

	// Pre-create as read-only — OpenFile in MdWriter.Append will fail with EACCES.
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Chmod(path, 0o400))

	base := time.Date(2026, 4, 28, 10, 30, 0, 0, time.UTC)
	r := NewMdRecorder(path)
	r.now = fakeClock(t, []time.Time{base})

	ctx := context.Background()
	const sid = "test-session"

	// Every method must return nil (E8 contract: render failures are non-halting).
	require.NoError(t, r.Open(ctx, Meta{SessionID: sid, Agent: "a", Model: "m", StartedAt: base}))
	require.NoError(t, r.UserMessage(ctx, sid, "hi"))

	tok, err := r.AgentEmit(ctx, sid, "echo hello")
	require.NoError(t, err)
	require.NoError(t, r.BashResult(ctx, tok, nil, 0, time.Millisecond))
	require.NoError(t, r.BashSkipped(ctx, tok, SevWarn, "blocked"))
	require.NoError(t, r.RuntimeEvent(ctx, sid, SevError, "provider error"))
	require.NoError(t, r.TurnEnd(ctx, sid))
	require.NoError(t, r.Close())
}
