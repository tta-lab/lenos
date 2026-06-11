package session_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/db"
	"github.com/tta-lab/lenos/internal/session"
)

func TestSave_RoundTripsLifetimeTotals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	conn, err := db.Connect(ctx, dir)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	svc := session.NewService(db.New(conn), conn)

	// Create a fresh session.
	sess, err := svc.Create(ctx, "test totals")
	require.NoError(t, err)

	// Set lifetime totals (simulating accumulated usage).
	sess.TotalPromptTokens = 1200
	sess.TotalCompletionTokens = 600
	sess.TotalReasoningTokens = 80
	sess.CacheCreationTokens = 40
	sess.CacheReadTokens = 200
	sess.CacheMissTokens = 800
	sess.Cost = 1.25

	// Save and re-read.
	saved, err := svc.Save(ctx, sess)
	require.NoError(t, err)

	reloaded, err := svc.Get(ctx, saved.ID)
	require.NoError(t, err)

	require.Equal(t, int64(1200), reloaded.TotalPromptTokens,
		"TotalPromptTokens must survive Save round-trip")
	require.Equal(t, int64(600), reloaded.TotalCompletionTokens,
		"TotalCompletionTokens must survive Save round-trip")
	require.Equal(t, int64(80), reloaded.TotalReasoningTokens,
		"TotalReasoningTokens must survive Save round-trip")
	require.Equal(t, int64(40), reloaded.CacheCreationTokens)
	require.Equal(t, int64(200), reloaded.CacheReadTokens)
	require.Equal(t, 1.25, reloaded.Cost)

	// Simulate second save accumulating more.
	reloaded.TotalPromptTokens += 400
	reloaded.TotalCompletionTokens += 200
	reloaded.TotalReasoningTokens += 20
	reloaded.Cost += 0.50

	saved2, err := svc.Save(ctx, reloaded)
	require.NoError(t, err)

	reloaded2, err := svc.Get(ctx, saved2.ID)
	require.NoError(t, err)

	require.Equal(t, int64(1600), reloaded2.TotalPromptTokens,
		"TotalPromptTokens should accumulate across saves")
	require.Equal(t, int64(800), reloaded2.TotalCompletionTokens,
		"TotalCompletionTokens should accumulate across saves")
	require.Equal(t, int64(100), reloaded2.TotalReasoningTokens,
		"TotalReasoningTokens should accumulate across saves")
	require.Equal(t, 1.75, reloaded2.Cost)
}

func TestSave_NewSessionTotalFieldsDefaultToZero(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	conn, err := db.Connect(ctx, dir)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	svc := session.NewService(db.New(conn), conn)

	sess, err := svc.Create(ctx, "zero test")
	require.NoError(t, err)

	// Re-read to confirm zero defaults round-trip.
	reloaded, err := svc.Get(ctx, sess.ID)
	require.NoError(t, err)

	require.Equal(t, int64(0), reloaded.TotalPromptTokens)
	require.Equal(t, int64(0), reloaded.TotalCompletionTokens)
	require.Equal(t, int64(0), reloaded.TotalReasoningTokens)
}

func TestSave_RoundTripsAgentAndLastModel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	conn, err := db.Connect(ctx, dir)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	svc := session.NewService(db.New(conn), conn)

	sess, err := svc.CreateWithMetadata(ctx, "review session", session.Metadata{
		AgentName: "reviewer",
		Provider:  "openai",
		Model:     "gpt-5",
	})
	require.NoError(t, err)

	require.Equal(t, "reviewer", sess.AgentName)
	require.Equal(t, "openai", sess.Provider)
	require.Equal(t, "gpt-5", sess.Model)

	sess.Provider = "anthropic"
	sess.Model = "claude-sonnet-4"
	saved, err := svc.Save(ctx, sess)
	require.NoError(t, err)

	reloaded, err := svc.Get(ctx, saved.ID)
	require.NoError(t, err)

	require.Equal(t, "reviewer", reloaded.AgentName)
	require.Equal(t, "anthropic", reloaded.Provider)
	require.Equal(t, "claude-sonnet-4", reloaded.Model)
}

func TestSave_CompactionZerosContextFieldsPreservesLifetimeTotals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	conn, err := db.Connect(ctx, dir)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	svc := session.NewService(db.New(conn), conn)

	// Create a session with accumulated pre-compaction state.
	sess, err := svc.Create(ctx, "compaction test")
	require.NoError(t, err)

	sess.TotalPromptTokens = 2000
	sess.TotalCompletionTokens = 1000
	sess.TotalReasoningTokens = 120
	sess.CacheCreationTokens = 60
	sess.CacheReadTokens = 400
	sess.CacheMissTokens = 800
	sess.Cost = 2.50
	// Current-context fields (latest turn).
	sess.PromptTokens = 300
	sess.CompletionTokens = 150

	saved, err := svc.Save(ctx, sess)
	require.NoError(t, err)
	require.Equal(t, int64(2000), saved.TotalPromptTokens)

	// Simulate compaction boundary: zero current-context fields,
	// preserve lifetime totals (exactly what agent_run.go does).
	saved.PromptTokens = 0
	saved.CompletionTokens = 0
	saved.CacheCreationTokens = 0
	saved.CacheReadTokens = 0
	saved.CacheMissTokens = 0

	compacted, err := svc.Save(ctx, saved)
	require.NoError(t, err)

	// Re-read to verify compaction didn't touch lifetime totals.
	reloaded, err := svc.Get(ctx, compacted.ID)
	require.NoError(t, err)

	require.Equal(t, int64(2000), reloaded.TotalPromptTokens,
		"TotalPromptTokens must survive compaction Save")
	require.Equal(t, int64(1000), reloaded.TotalCompletionTokens,
		"TotalCompletionTokens must survive compaction Save")
	require.Equal(t, int64(120), reloaded.TotalReasoningTokens,
		"TotalReasoningTokens must survive compaction Save")
	require.Equal(t, 2.50, reloaded.Cost,
		"Cost must survive compaction Save")

	// Context fields should be zeroed as compaction intended.
	require.Equal(t, int64(0), reloaded.PromptTokens)
	require.Equal(t, int64(0), reloaded.CompletionTokens)
	require.Equal(t, int64(0), reloaded.CacheCreationTokens)
	require.Equal(t, int64(0), reloaded.CacheReadTokens)
	require.Equal(t, int64(0), reloaded.CacheMissTokens)

	// Post-compaction usage accumulates on top of lifetime totals.
	reloaded.TotalPromptTokens += 500
	reloaded.TotalCompletionTokens += 250
	reloaded.TotalReasoningTokens += 30
	reloaded.Cost += 0.75

	updated, err := svc.Save(ctx, reloaded)
	require.NoError(t, err)

	reloaded2, err := svc.Get(ctx, updated.ID)
	require.NoError(t, err)

	require.Equal(t, int64(2500), reloaded2.TotalPromptTokens,
		"Post-compaction total_prompt_tokens should accumulate from survived total")
	require.Equal(t, int64(1250), reloaded2.TotalCompletionTokens)
	require.Equal(t, int64(150), reloaded2.TotalReasoningTokens)
	require.Equal(t, 3.25, reloaded2.Cost)
}
