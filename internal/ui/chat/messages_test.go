package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tta-lab/lenos/internal/agent/lenosbash"
	"github.com/tta-lab/lenos/internal/message"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

func TestExtractMessageItems_Assistant_EmptyContent(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	msg := &message.Message{
		ID:    "test-empty-assistant",
		Role:  message.Assistant,
		Parts: []message.ContentPart{message.TextContent{Text: ""}},
	}
	msg.AddFinish(message.FinishReasonEndTurn, "", "")
	items := ExtractMessageItems(&sty, msg, false)
	require.Len(t, items, 1, "empty-content assistant message must produce exactly one MessageItem")
	_, ok := items[0].(*AssistantMessageItem)
	assert.True(t, ok, "item must be an AssistantMessageItem")
}

func TestExtractMessageItems_Result_SkipsCompletedSuccessfulCommandWithoutOutput(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	exitCode := 0
	msg := &message.Message{
		ID:   "result-success",
		Role: message.Result,
		Parts: []message.ContentPart{
			message.CommandContent{
				Command:  "echo ok",
				Output:   "ok\n",
				ExitCode: &exitCode,
				Pending:  false,
			},
		},
	}
	items := ExtractMessageItems(&sty, msg, false)
	assert.Empty(t, items, "completed successful command result is already represented by the assistant bash emit")
}

func TestExtractMessageItems_Result_SkipsCompletedSuccessfulCommand(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	exitCode := 0
	msg := &message.Message{
		ID:   "result-mixed-success",
		Role: message.Result,
		Parts: []message.ContentPart{
			message.CommandContent{
				Command:  "cat main.go",
				Output:   "",
				ExitCode: &exitCode,
				Pending:  false,
			},
		},
	}
	items := ExtractMessageItems(&sty, msg, false)
	assert.Empty(t, items)
}

func TestExtractMessageItems_Result_KeepsNonZeroResult(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	exitCode := 1
	msg := &message.Message{
		ID:   "result-mixed-fail",
		Role: message.Result,
		Parts: []message.ContentPart{
			message.CommandContent{
				Command:  "cat missing.go",
				Output:   "No such file",
				ExitCode: &exitCode,
				Pending:  false,
			},
		},
	}
	items := ExtractMessageItems(&sty, msg, false)
	// Non-zero exit: still renders for failure output.
	require.Len(t, items, 1)
	_, ok := items[0].(*ResultMessageItem)
	assert.True(t, ok)
}

func TestExtractMessageItems_Result_SkipsEmptyCommandContent(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	msg := &message.Message{
		ID:   "result-empty-command",
		Role: message.Result,
		Parts: []message.ContentPart{
			message.CommandContent{},
		},
	}
	items := ExtractMessageItems(&sty, msg, false)
	assert.Empty(t, items)
}

func TestExtractMessageItems_Result_KeepsVisibleResultRows(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	exitCode := 1
	cases := []struct {
		name string
		msg  *message.Message
	}{
		{
			name: "pending command",
			msg: &message.Message{
				ID:   "pending",
				Role: message.Result,
				Parts: []message.ContentPart{
					message.CommandContent{Command: "sleep 1", Pending: true},
				},
			},
		},
		{
			name: "failed command",
			msg: &message.Message{
				ID:   "failed",
				Role: message.Result,
				Parts: []message.ContentPart{
					message.CommandContent{Command: "false", ExitCode: &exitCode, Pending: false},
				},
			},
		},
		{
			name: "completed command without exit code",
			msg: &message.Message{
				ID:   "unknown-exit",
				Role: message.Result,
				Parts: []message.ContentPart{
					message.CommandContent{Command: "legacy command", Pending: false},
				},
			},
		},
		{
			name: "runtime text response",
			msg: &message.Message{
				ID:    "runtime",
				Role:  message.Result,
				Parts: []message.ContentPart{message.TextContent{Text: lenosbash.RuntimeLine("retry")}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			items := ExtractMessageItems(&sty, tc.msg, false)
			require.Len(t, items, 1)
			_, ok := items[0].(*ResultMessageItem)
			assert.True(t, ok)
		})
	}
}
