package message

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshalUnmarshalParts(t *testing.T) {
	t.Parallel()

	t.Run("ReasoningContent", func(t *testing.T) {
		t.Parallel()
		parts := []ContentPart{
			ReasoningContent{Thinking: "Let me think step by step", Signature: "sig123"},
		}
		data, err := marshalParts(parts)
		require.NoError(t, err)
		got, err := unmarshalParts(data)
		require.NoError(t, err)
		require.Len(t, got, 1)
		rc, ok := got[0].(ReasoningContent)
		require.True(t, ok)
		require.Equal(t, "Let me think step by step", rc.Thinking)
		require.Equal(t, "sig123", rc.Signature)
	})

	t.Run("TextContent", func(t *testing.T) {
		t.Parallel()
		parts := []ContentPart{TextContent{Text: "Hello, world!"}}
		data, err := marshalParts(parts)
		require.NoError(t, err)
		got, err := unmarshalParts(data)
		require.NoError(t, err)
		require.Len(t, got, 1)
		tc, ok := got[0].(TextContent)
		require.True(t, ok)
		require.Equal(t, "Hello, world!", tc.Text)
	})

	t.Run("CommandContent pending", func(t *testing.T) {
		t.Parallel()
		parts := []ContentPart{
			CommandContent{Command: "echo hello", Pending: true},
		}
		data, err := marshalParts(parts)
		require.NoError(t, err)
		got, err := unmarshalParts(data)
		require.NoError(t, err)
		require.Len(t, got, 1)
		cc, ok := got[0].(CommandContent)
		require.True(t, ok)
		require.Equal(t, "echo hello", cc.Command)
		require.True(t, cc.Pending)
		require.Empty(t, cc.Output)
		require.Nil(t, cc.ExitCode)
	})

	t.Run("CommandContent completed with exit code", func(t *testing.T) {
		t.Parallel()
		exitCode := 0
		parts := []ContentPart{
			CommandContent{Command: "go build .", Output: "Build succeeded", ExitCode: &exitCode},
		}
		data, err := marshalParts(parts)
		require.NoError(t, err)
		got, err := unmarshalParts(data)
		require.NoError(t, err)
		require.Len(t, got, 1)
		cc, ok := got[0].(CommandContent)
		require.True(t, ok)
		require.Equal(t, "go build .", cc.Command)
		require.Equal(t, "Build succeeded", cc.Output)
		require.False(t, cc.Pending)
		require.NotNil(t, cc.ExitCode)
		require.Equal(t, 0, *cc.ExitCode)
	})

	t.Run("CommandContent with non-zero exit code", func(t *testing.T) {
		t.Parallel()
		exitCode := 127
		parts := []ContentPart{
			CommandContent{Command: "ls /nonexistent", Output: "ls: /nonexistent: No such file or directory", ExitCode: &exitCode},
		}
		data, err := marshalParts(parts)
		require.NoError(t, err)
		got, err := unmarshalParts(data)
		require.NoError(t, err)
		require.Len(t, got, 1)
		cc, ok := got[0].(CommandContent)
		require.True(t, ok)
		require.NotNil(t, cc.ExitCode)
		require.Equal(t, 127, *cc.ExitCode)
	})

	t.Run("ImageURLContent", func(t *testing.T) {
		t.Parallel()
		parts := []ContentPart{ImageURLContent{URL: "https://example.com/image.png", Detail: "high"}}
		data, err := marshalParts(parts)
		require.NoError(t, err)
		got, err := unmarshalParts(data)
		require.NoError(t, err)
		require.Len(t, got, 1)
		iu, ok := got[0].(ImageURLContent)
		require.True(t, ok)
		require.Equal(t, "https://example.com/image.png", iu.URL)
		require.Equal(t, "high", iu.Detail)
	})

	t.Run("BinaryContent", func(t *testing.T) {
		t.Parallel()
		data := []byte{0x89, 0x50, 0x4E, 0x47}
		parts := []ContentPart{BinaryContent{Path: "/tmp/screenshot.png", MIMEType: "image/png", Data: data}}
		serialized, err := marshalParts(parts)
		require.NoError(t, err)
		got, err := unmarshalParts(serialized)
		require.NoError(t, err)
		require.Len(t, got, 1)
		bc, ok := got[0].(BinaryContent)
		require.True(t, ok)
		require.Equal(t, "/tmp/screenshot.png", bc.Path)
		require.Equal(t, "image/png", bc.MIMEType)
		require.Equal(t, data, bc.Data)
	})

	t.Run("ToolCall", func(t *testing.T) {
		t.Parallel()
		parts := []ContentPart{
			ToolCall{ID: "tool_1", Name: "Bash", Input: `{"command":"ls"}`, Finished: true},
		}
		data, err := marshalParts(parts)
		require.NoError(t, err)
		got, err := unmarshalParts(data)
		require.NoError(t, err)
		require.Len(t, got, 1)
		tc, ok := got[0].(ToolCall)
		require.True(t, ok)
		require.Equal(t, "tool_1", tc.ID)
		require.Equal(t, "Bash", tc.Name)
		require.Equal(t, `{"command":"ls"}`, tc.Input)
		require.True(t, tc.Finished)
	})

	t.Run("ToolResult", func(t *testing.T) {
		t.Parallel()
		parts := []ContentPart{
			ToolResult{ToolCallID: "tool_1", Name: "Bash", Content: "file1.go\nfile2.go", Data: "", MIMEType: "text/plain"},
		}
		data, err := marshalParts(parts)
		require.NoError(t, err)
		got, err := unmarshalParts(data)
		require.NoError(t, err)
		require.Len(t, got, 1)
		tr, ok := got[0].(ToolResult)
		require.True(t, ok)
		require.Equal(t, "tool_1", tr.ToolCallID)
		require.Equal(t, "Bash", tr.Name)
		require.Equal(t, "file1.go\nfile2.go", tr.Content)
	})

	t.Run("Finish", func(t *testing.T) {
		t.Parallel()
		parts := []ContentPart{Finish{Reason: FinishReasonEndTurn, Message: "done"}}
		data, err := marshalParts(parts)
		require.NoError(t, err)
		got, err := unmarshalParts(data)
		require.NoError(t, err)
		require.Len(t, got, 1)
		f, ok := got[0].(Finish)
		require.True(t, ok)
		require.Equal(t, FinishReasonEndTurn, f.Reason)
		require.Equal(t, "done", f.Message)
	})

	t.Run("mixed parts", func(t *testing.T) {
		t.Parallel()
		exitCode := 42
		parts := []ContentPart{
			TextContent{Text: "Running command..."},
			CommandContent{Command: "cargo test", Output: "test result: ok", ExitCode: &exitCode},
			TextContent{Text: "Done."},
		}
		data, err := marshalParts(parts)
		require.NoError(t, err)
		got, err := unmarshalParts(data)
		require.NoError(t, err)
		require.Len(t, got, 3)

		tc1, ok := got[0].(TextContent)
		require.True(t, ok)
		require.Equal(t, "Running command...", tc1.Text)

		cc, ok := got[1].(CommandContent)
		require.True(t, ok)
		require.Equal(t, "cargo test", cc.Command)
		require.Equal(t, "test result: ok", cc.Output)

		tc2, ok := got[2].(TextContent)
		require.True(t, ok)
		require.Equal(t, "Done.", tc2.Text)
	})
}
