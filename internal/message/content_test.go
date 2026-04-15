package message

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

func makeTestAttachments(n int, contentSize int) []Attachment {
	attachments := make([]Attachment, n)
	content := []byte(strings.Repeat("x", contentSize))
	for i := range n {
		attachments[i] = Attachment{
			FilePath: fmt.Sprintf("/path/to/file%d.txt", i),
			MimeType: "text/plain",
			Content:  content,
		}
	}
	return attachments
}

func BenchmarkPromptWithTextAttachments(b *testing.B) {
	cases := []struct {
		name        string
		numFiles    int
		contentSize int
	}{
		{"1file_100bytes", 1, 100},
		{"5files_1KB", 5, 1024},
		{"10files_10KB", 10, 10 * 1024},
		{"20files_50KB", 20, 50 * 1024},
	}

	for _, tc := range cases {
		attachments := makeTestAttachments(tc.numFiles, tc.contentSize)
		prompt := "Process these files"

		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_ = PromptWithTextAttachments(prompt, attachments)
			}
		})
	}
}

func TestToAIMessage_Result(t *testing.T) {
	t.Parallel()

	exitCode := 0
	msg := Message{
		Role: Result,
		Parts: []ContentPart{
			CommandContent{Command: "ls -la", Output: "total 8\n-rw-r--  1 neil staff 256 Apr 12 test", ExitCode: &exitCode, Pending: false},
		},
	}
	result := msg.ToAIMessage()
	require.Len(t, result, 1)
	require.Equal(t, fantasy.MessageRoleUser, result[0].Role)
	require.Len(t, result[0].Content, 1)
	text, ok := result[0].Content[0].(fantasy.TextPart)
	require.True(t, ok, "expected TextPart, got %T", result[0].Content[0])
	require.Contains(t, text.Text, "total 8")
}

func TestToAIMessage_ResultMultiCommand(t *testing.T) {
	t.Parallel()

	exit0, exit1 := 0, 1
	msg := Message{
		Role: Result,
		Parts: []ContentPart{
			CommandContent{Command: "ls", Output: `file1
file2`, ExitCode: &exit0, Pending: false},
			CommandContent{Command: "pwd", Output: "/home/user", ExitCode: &exit1, Pending: false},
		},
	}
	result := msg.ToAIMessage()
	require.Len(t, result, 1)
	require.Equal(t, fantasy.MessageRoleUser, result[0].Role)
	text, ok := result[0].Content[0].(fantasy.TextPart)
	require.True(t, ok, "expected TextPart, got %T", result[0].Content[0])
	// logos v2.1.0 drops command echo from result blocks — only output is included.
	require.Contains(t, text.Text, "file1")
	require.Contains(t, text.Text, "/home/user")
}

func TestToAIMessage_Result_PendingOnly(t *testing.T) {
	t.Parallel()

	msg := Message{
		Role: Result,
		Parts: []ContentPart{
			CommandContent{Command: "ls", Output: "", Pending: true},
			CommandContent{Command: "pwd", Output: "", Pending: true},
		},
	}
	result := msg.ToAIMessage()
	// Pending commands produce zero AI messages — they are still running.
	require.Len(t, result, 0)
}

func TestToAIMessage_Result_MixedPendingAndCompleted(t *testing.T) {
	t.Parallel()

	exit0 := 0
	msg := Message{
		Role: Result,
		Parts: []ContentPart{
			CommandContent{Command: "ls", Output: `file1\nfile2`, ExitCode: &exit0, Pending: false},
			CommandContent{Command: "pwd", Output: "", Pending: true},
		},
	}
	result := msg.ToAIMessage()
	// Only the completed command should appear in the AI message.
	require.Len(t, result, 1)
	text, ok := result[0].Content[0].(fantasy.TextPart)
	require.True(t, ok, "expected TextPart, got %T", result[0].Content[0])
	// logos v2.1.0 drops command echo — check output content instead.
	require.Contains(t, text.Text, "file1")
	require.Contains(t, text.Text, "file2")
}

func TestToAIMessage_Assistant_ReasoningBeforeText(t *testing.T) {
	t.Parallel()

	msg := Message{
		Role: Assistant,
		Parts: []ContentPart{
			TextContent{Text: "Hello, world!"},
			ReasoningContent{Thinking: "I should greet the user", Signature: "sig123"},
		},
	}
	result := msg.ToAIMessage()
	require.Len(t, result, 1)
	require.Equal(t, fantasy.MessageRoleAssistant, result[0].Role)
	require.Len(t, result[0].Content, 2)
	// Reasoning must come before text for Anthropic signature validation.
	_, ok := result[0].Content[0].(fantasy.ReasoningPart)
	require.True(t, ok, "first part should be ReasoningPart, got %T", result[0].Content[0])
	text, ok := result[0].Content[1].(fantasy.TextPart)
	require.True(t, ok, "second part should be TextPart, got %T", result[0].Content[1])
	require.Equal(t, "Hello, world!", text.Text)
}
