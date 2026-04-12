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
	require.Contains(t, text.Text, "ls -la")
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
	// FormatResults should include both commands.
	require.Contains(t, text.Text, "ls")
	require.Contains(t, text.Text, "pwd")
}
