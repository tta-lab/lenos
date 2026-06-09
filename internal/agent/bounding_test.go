package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/config"
)

func TestBoundOutput_NilLimits(t *testing.T) {
	t.Parallel()
	out := boundOutput([]byte("hello"), nil, "")
	require.Equal(t, "hello", out.Preview)
	require.Empty(t, out.FullPath)
}

func TestBoundOutput_FitsBothLimits(t *testing.T) {
	t.Parallel()
	limits := &config.BashOutputConfig{MaxLines: 2000, MaxBytes: 51200}
	out := boundOutput([]byte("hello"), limits, "")
	require.Equal(t, "hello", out.Preview)
	require.Empty(t, out.FullPath)
}

func TestBoundOutput_ExceedsLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	limits := &config.BashOutputConfig{MaxLines: 5, MaxBytes: 51200}

	// Build content with 10 lines.
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}

	out := boundOutput([]byte(sb.String()), limits, dir)
	require.Contains(t, out.Preview, "...bash output truncated")
	require.Contains(t, out.Preview, "Full output saved to:")
	require.NotEmpty(t, out.FullPath)
	require.FileExists(t, out.FullPath)
}

func TestBoundOutput_ExceedsBytes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	limits := &config.BashOutputConfig{MaxLines: 2000, MaxBytes: 100}

	// Build content >100 bytes.
	content := []byte(strings.Repeat("hello world ", 50))

	out := boundOutput(content, limits, dir)
	require.Contains(t, out.Preview, "...bash output truncated")
	require.NotEmpty(t, out.FullPath)
	require.FileExists(t, out.FullPath)
}

func TestBoundOutput_FileContentPreserved(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	limits := &config.BashOutputConfig{MaxLines: 3, MaxBytes: 1000}

	input := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	out := boundOutput([]byte(input), limits, dir)

	b, err := os.ReadFile(out.FullPath)
	require.NoError(t, err)
	require.Equal(t, input, string(b))
}

func TestCountLines(t *testing.T) {
	t.Parallel()
	require.Equal(t, 0, countLines([]byte("")))
	require.Equal(t, 1, countLines([]byte("hello")))
	require.Equal(t, 2, countLines([]byte("hello\nworld")))
	require.Equal(t, 2, countLines([]byte("hello\nworld\n")))
}

func TestTruncateToValidUTF8(t *testing.T) {
	t.Parallel()
	// No truncation needed.
	require.Equal(t, "abc", truncateToValidUTF8([]byte("abc"), 10))
	// Exact boundary.
	require.Equal(t, "abc", truncateToValidUTF8([]byte("abc"), 3))
	// Multi-byte safe.
	s := "héllo"
	b := []byte(s)
	truncated := truncateToValidUTF8(b, 3)
	require.True(t, strings.HasPrefix(s, truncated))
	// Verify no broken multi-byte sequence.
	for _, r := range truncated {
		require.NotEqual(t, r, rune(0xFFFD), "no replacement characters")
	}
}

func TestBashOutputDir(t *testing.T) {
	t.Parallel()
	dir := bashOutputDir("/work/.lenos")
	require.Equal(t, filepath.Join("/work/.lenos", "bash-output"), dir)
}

func TestBuildTailBiasedPreview(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	content := []byte(sb.String())

	// Should only get tail lines.
	preview := buildTailBiasedPreview(content, 10, 102400)
	lines := strings.Split(strings.TrimRight(preview, "\n"), "\n")
	require.LessOrEqual(t, len(lines), 8) // 80% of 10 = 8
	// Tail-biased should include later lines.
	require.Contains(t, preview, "line 19")
}

func TestBoundOutput_StderrBounded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	limits := &config.BashOutputConfig{MaxLines: 2, MaxBytes: 1000}

	stderr := "error line 1\nerror line 2\nerror line 3\nerror line 4\nerror line 5\n"
	out := boundOutput([]byte(stderr), limits, dir)

	require.Contains(t, out.Preview, "...bash output truncated")
	require.Contains(t, out.Preview, "Full output saved to:")
	require.NotEmpty(t, out.FullPath)
	require.FileExists(t, out.FullPath)

	b, err := os.ReadFile(out.FullPath)
	require.NoError(t, err)
	require.Equal(t, stderr, string(b))
}
