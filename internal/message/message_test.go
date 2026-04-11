package message

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshalUnmarshalParts(t *testing.T) {
	t.Parallel()

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
