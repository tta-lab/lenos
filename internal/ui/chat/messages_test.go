package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestExtractMessageItems_Result_SkipsCompletedSuccessfulCommand(t *testing.T) {
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

func TestExtractMessageItems_Result_KeepsSuccessfulCommandWithNarration(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	exitCode := 0
	msg := &message.Message{
		ID:   "result-narration",
		Role: message.Result,
		Parts: []message.ContentPart{
			message.CommandContent{
				Command:  "cat <<'EOF' | narrate\nDone.\nEOF",
				ExitCode: &exitCode,
				Pending:  false,
				Narrations: []message.CommandNarration{
					{Body: "Done."},
				},
			},
		},
	}

	items := ExtractMessageItems(&sty, msg, false)

	require.Len(t, items, 1)
	_, ok := items[0].(*ResultMessageItem)
	assert.False(t, ok, "narration body should not render as a bash result row")

	rendered := items[0].Render(80)
	assert.True(t, strings.HasPrefix(rendered, sty.Chat.Message.AssistantBlurred.Render()))
	assert.Contains(t, ansi.Strip(rendered), "Done")
	assert.NotContains(t, ansi.Strip(rendered), "$ narrate")
}

func TestExtractMessageItems_Result_SplitsFailedCommandAndNarration(t *testing.T) {
	t.Parallel()
	sty := styles.DefaultStyles()
	exitCode := 1
	msg := &message.Message{
		ID:   "result-failure-narration",
		Role: message.Result,
		Parts: []message.ContentPart{
			message.CommandContent{
				Command:  "false; cat <<'EOF' | narrate\n# Failed\nEOF",
				Output:   "command failed",
				ExitCode: &exitCode,
				Pending:  false,
				Narrations: []message.CommandNarration{
					{Body: "# Failed\nshown to human"},
				},
			},
		},
	}

	items := ExtractMessageItems(&sty, msg, false)

	require.Len(t, items, 2)
	_, ok := items[0].(*ResultMessageItem)
	require.True(t, ok)
	assert.Contains(t, ansi.Strip(items[0].Render(80)), "command failed")

	_, ok = items[1].(*ResultMessageItem)
	assert.False(t, ok, "narration body should render with assistant markdown treatment")
	rendered := items[1].Render(80)
	assert.True(t, strings.HasPrefix(rendered, sty.Chat.Message.AssistantBlurred.Render()))
	assert.Contains(t, ansi.Strip(rendered), "Failed")
	assert.Contains(t, ansi.Strip(rendered), "shown to human")
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
				Parts: []message.ContentPart{message.TextContent{Text: "[runtime] retry"}},
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
