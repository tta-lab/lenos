package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/db"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/logos"
)

func TestStepsToMessages(t *testing.T) {
	conn, err := db.Connect(t.Context(), t.TempDir())
	require.NoError(t, err)
	defer conn.Close()
	q := db.New(conn)
	svc := message.NewService(q)
	sessSvc := session.NewService(q, conn)
	sessionID := "test-session"

	// Create a test session first (required for message FK).
	_, err = sessSvc.Create(context.Background(), "Test Session")
	require.NoError(t, err)
	// Get the actual session ID.
	sessions, err := sessSvc.List(context.Background())
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	sessionID = sessions[0].ID

	t.Run("assistant plain text", func(t *testing.T) {
		steps := []logos.StepMessage{
			{Role: logos.StepRoleAssistant, Content: "Hello world"},
		}
		err := stepsToMessages(context.Background(), steps, sessionID, svc)
		require.NoError(t, err)
		msgs, err := svc.List(context.Background(), sessionID)
		require.NoError(t, err)
		require.Equal(t, message.Assistant, msgs[0].Role)
		text := msgs[0].Content()
		require.Equal(t, "Hello world", text.Text)
	})

	t.Run("assistant with reasoning", func(t *testing.T) {
		steps := []logos.StepMessage{
			{
				Role:               logos.StepRoleAssistant,
				Content:            "Final answer",
				Reasoning:          "I thought about it",
				ReasoningSignature: "sig123",
			},
		}
		err := stepsToMessages(context.Background(), steps, sessionID, svc)
		require.NoError(t, err)
		msgs, err := svc.List(context.Background(), sessionID)
		require.NoError(t, err)
		last := msgs[len(msgs)-1]
		require.Equal(t, message.Assistant, last.Role)
		reasoning := last.ReasoningContent()
		require.Equal(t, "I thought about it", reasoning.Thinking)
		require.Equal(t, "sig123", reasoning.Signature)
	})

	t.Run("result role", func(t *testing.T) {
		steps := []logos.StepMessage{
			{
				Role:      logos.StepRoleResult,
				Content:   "command output here",
				Timestamp: time.Now(),
			},
		}
		err := stepsToMessages(context.Background(), steps, sessionID, svc)
		require.NoError(t, err)
		msgs, err := svc.List(context.Background(), sessionID)
		require.NoError(t, err)
		last := msgs[len(msgs)-1]
		require.Equal(t, message.Result, last.Role)
		require.Contains(t, last.Content().Text, "command output here")
	})

	t.Run("user role", func(t *testing.T) {
		steps := []logos.StepMessage{
			{Role: logos.StepRoleUser, Content: "user input"},
		}
		err := stepsToMessages(context.Background(), steps, sessionID, svc)
		require.NoError(t, err)
		msgs, err := svc.List(context.Background(), sessionID)
		require.NoError(t, err)
		last := msgs[len(msgs)-1]
		require.Equal(t, message.User, last.Role)
		require.Equal(t, "user input", last.Content().Text)
	})
}
