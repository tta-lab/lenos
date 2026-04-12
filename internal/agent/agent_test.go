package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/logos"

	_ "github.com/joho/godotenv/autoload"
)

func makeTestTodos(n int) []session.Todo {
	todos := make([]session.Todo, n)
	for i := range n {
		todos[i] = session.Todo{
			Status:  session.TodoStatusPending,
			Content: fmt.Sprintf("Task %d: Implement feature with some description that makes it realistic", i),
		}
	}
	return todos
}

func TestBuildSummaryPrompt(t *testing.T) {
	t.Parallel()

	t.Run("empty jobID returns base prompt without todos section", func(t *testing.T) {
		t.Parallel()
		result := buildSummaryPrompt(context.Background(), "")
		require.Contains(t, result, "Provide a detailed summary of our conversation above.")
		require.NotContains(t, result, "Current Todo List")
	})

	t.Run("cancelled context returns base prompt", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result := buildSummaryPrompt(ctx, "fake-nonexistent-job-id")
		require.Contains(t, result, "Provide a detailed summary of our conversation above.")
		require.NotContains(t, result, "Current Todo List")
	})

	t.Run("successful poll with real jobID returns prompt", func(t *testing.T) {
		t.Parallel()
		// Use a UUID unlikely to have subtasks in a fresh test environment.
		// task will return empty output, so todos section won't appear but
		// the base prompt will.
		result := buildSummaryPrompt(context.Background(), "00000000-0000-0000-0000-000000000000")
		require.Contains(t, result, "Provide a detailed summary of our conversation above.")
	})
}

func TestGenerateTitle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("task CLI not available on windows")
	}
	if _, err := os.LookupEnv("TTAL_JOB_ID"); !err {
		t.Setenv("TTAL_JOB_ID", "25620b89")
	}

	t.Run("uses task description as session title", func(t *testing.T) {
		env := testEnv(t)
		sess, err := env.sessions.Create(t.Context(), "Untitled Session")
		require.NoError(t, err)

		// Create a fake "task" binary that outputs a JSON array.
		tmp := t.TempDir()
		oldPath := os.Getenv("PATH")
		t.Cleanup(func() { os.Setenv("PATH", oldPath) })
		os.Setenv("PATH", tmp+string(os.PathListSeparator)+oldPath)

		fakeTask := filepath.Join(tmp, "task")
		require.NoError(t, os.WriteFile(fakeTask, []byte(fmt.Sprintf(`#!/bin/sh
printf '[{"description":"%s","status":"pending"}]' "$@"
`, "Fix the authentication bug")), 0o755))

		a := &sessionAgent{sessions: env.sessions}
		a.generateTitle(t.Context(), sess.ID, "fix auth bug")

		updated, err := env.sessions.Get(t.Context(), sess.ID)
		require.NoError(t, err)
		assert.Equal(t, "Fix the authentication bug", updated.Title)
	})

	t.Run("empty array falls back to default", func(t *testing.T) {
		env := testEnv(t)
		sess, err := env.sessions.Create(t.Context(), "Untitled Session")
		require.NoError(t, err)

		tmp := t.TempDir()
		oldPath := os.Getenv("PATH")
		t.Cleanup(func() { os.Setenv("PATH", oldPath) })
		os.Setenv("PATH", tmp+string(os.PathListSeparator)+oldPath)

		fakeTask := filepath.Join(tmp, "task")
		require.NoError(t, os.WriteFile(fakeTask, []byte("#!/bin/sh\nprintf '[]' \"$@\""), 0o755))

		a := &sessionAgent{sessions: env.sessions}
		a.generateTitle(t.Context(), sess.ID, "")

		updated, err := env.sessions.Get(t.Context(), sess.ID)
		require.NoError(t, err)
		assert.Equal(t, DefaultSessionName, updated.Title)
	})

	t.Run("no TTAL_JOB_ID uses default", func(t *testing.T) {
		env := testEnv(t)
		sess, err := env.sessions.Create(t.Context(), "Untitled Session")
		require.NoError(t, err)

		t.Setenv("TTAL_JOB_ID", "")

		a := &sessionAgent{sessions: env.sessions}
		a.generateTitle(t.Context(), sess.ID, "")

		updated, err := env.sessions.Get(t.Context(), sess.ID)
		require.NoError(t, err)
		assert.Equal(t, DefaultSessionName, updated.Title)
	})

	t.Run("description over 100 chars is truncated", func(t *testing.T) {
		env := testEnv(t)
		sess, err := env.sessions.Create(t.Context(), "Untitled Session")
		require.NoError(t, err)

		tmp := t.TempDir()
		oldPath := os.Getenv("PATH")
		t.Cleanup(func() { os.Setenv("PATH", oldPath) })
		os.Setenv("PATH", tmp+string(os.PathListSeparator)+oldPath)

		longDesc := strings.Repeat("x", 150)
		fakeTask := filepath.Join(tmp, "task")
		require.NoError(t, os.WriteFile(fakeTask, []byte(fmt.Sprintf("#!/bin/sh\nprintf '[{\"description\":\"%s\",\"status\":\"pending\"}]' \"$@\"", longDesc)), 0o755))

		a := &sessionAgent{sessions: env.sessions}
		a.generateTitle(t.Context(), sess.ID, "")

		updated, err := env.sessions.Get(t.Context(), sess.ID)
		require.NoError(t, err)
		assert.Len(t, updated.Title, 100)
		assert.Equal(t, longDesc[:100], updated.Title)
	})
}

func TestBackfillReasoning(t *testing.T) {
	t.Parallel()

	t.Run("empty assistant list logs mismatch warning without panic", func(t *testing.T) {
		env := testEnv(t)
		sess, err := env.sessions.Create(t.Context(), "Test Session")
		require.NoError(t, err)

		// Create assistant messages in the DB.
		msg1, err := env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
			Role:  message.Assistant,
			Parts: []message.ContentPart{},
		})
		require.NoError(t, err)
		msg2, err := env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
			Role:  message.Assistant,
			Parts: []message.ContentPart{},
		})
		require.NoError(t, err)

		// logos returns assistant steps but createdAssistantMsgs is empty
		// (simulates pure-cmd turn where no assistant messages were created).
		result := &logos.RunResult{
			Steps: []logos.StepMessage{
				{Role: logos.StepRoleAssistant, Reasoning: "Thinking", ReasoningSignature: "sig"},
			},
		}

		a := &sessionAgent{
			messages: env.messages,
		}
		// Should not panic.
		a.backfillReasoning(t.Context(), result, []*message.Message{})

		// Verify the actual messages were NOT updated (since createdAssistantMsgs was empty).
		updated1, err := env.messages.Get(t.Context(), msg1.ID)
		require.NoError(t, err)
		require.Empty(t, updated1.ReasoningContent().Thinking)

		updated2, err := env.messages.Get(t.Context(), msg2.ID)
		require.NoError(t, err)
		require.Empty(t, updated2.ReasoningContent().Thinking)
	})

	t.Run("skipped step with empty reasoning still pairs correctly", func(t *testing.T) {
		env := testEnv(t)
		sess, err := env.sessions.Create(t.Context(), "Test Session")
		require.NoError(t, err)

		// Create three assistant messages.
		msg1, err := env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
			Role:  message.Assistant,
			Parts: []message.ContentPart{},
		})
		require.NoError(t, err)
		msg2, err := env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
			Role:  message.Assistant,
			Parts: []message.ContentPart{},
		})
		require.NoError(t, err)
		msg3, err := env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
			Role:  message.Assistant,
			Parts: []message.ContentPart{},
		})
		require.NoError(t, err)

		createdMsgs := []*message.Message{&msg1, &msg2, &msg3}

		// Middle step has no reasoning — should be skipped but pairing stays aligned.
		result := &logos.RunResult{
			Steps: []logos.StepMessage{
				{Role: logos.StepRoleUser, Content: "step 0"},
				{Role: logos.StepRoleAssistant, Reasoning: "first", ReasoningSignature: "sig1"},
				{Role: logos.StepRoleUser, Content: "step 1"},
				{Role: logos.StepRoleAssistant, Reasoning: "", ReasoningSignature: ""}, // skipped
				{Role: logos.StepRoleUser, Content: "step 2"},
				{Role: logos.StepRoleAssistant, Reasoning: "third", ReasoningSignature: "sig3"},
			},
		}

		a := &sessionAgent{
			messages: env.messages,
		}
		a.backfillReasoning(t.Context(), result, createdMsgs)

		// Verify msg1 got reasoning.
		updated1, err := env.messages.Get(t.Context(), msg1.ID)
		require.NoError(t, err)
		require.Equal(t, "first", updated1.ReasoningContent().Thinking)

		// Verify msg2 was skipped (no reasoning added).
		updated2, err := env.messages.Get(t.Context(), msg2.ID)
		require.NoError(t, err)
		require.Empty(t, updated2.ReasoningContent().Thinking)

		// Verify msg3 got reasoning (pairing stays aligned despite skipped step).
		updated3, err := env.messages.Get(t.Context(), msg3.ID)
		require.NoError(t, err)
		require.Equal(t, "third", updated3.ReasoningContent().Thinking)
	})
}
