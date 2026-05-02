package transcript

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatcher(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fsnotify behaviour differs on Windows")
	}

	// fsnotify watcher is not safe to parallelize — disable to prevent races.
	tmp := t.TempDir()
	mdPath := filepath.Join(tmp, "session.md")

	// Write initial content.
	require.NoError(t, os.WriteFile(mdPath, []byte("# Session\n"), 0o644))

	initial, w, err := NewWatcher(mdPath, 5*time.Millisecond)
	require.NoError(t, err)

	assert.Equal(t, []byte("# Session\n"), initial)

	// Append a bash block.
	require.NoError(t, os.WriteFile(mdPath, []byte("# Session\n\n```bash\ngo build\n```\n"), 0o644))

	msg := w.Next()
	appendMsg, ok := msg.(WatchAppend)
	require.True(t, ok, "expected WatchAppend, got %T", msg)
	assert.Equal(t, []byte("\n```bash\ngo build\n```\n"), appendMsg.Bytes)

	// Multiple writes within debounce window coalesce.
	require.NoError(t, os.WriteFile(mdPath, []byte("# Session\n\n```bash\ngo build\n```\n\noutput\n"), 0o644))
	require.NoError(t, os.WriteFile(mdPath, []byte("# Session\n\n```bash\ngo build\n```\n\noutput\nerror\n"), 0o644))

	msg = w.Next()
	appendMsg, ok = msg.(WatchAppend)
	require.True(t, ok)
	// Should contain all new bytes since last offset.
	assert.Contains(t, string(appendMsg.Bytes), "error")

	w.Close()
}

func TestWatcherTruncation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fsnotify behaviour differs on Windows")
	}

	// fsnotify watcher is not safe to parallelize — disable to prevent races.
	tmp := t.TempDir()
	mdPath := filepath.Join(tmp, "session.md")

	require.NoError(t, os.WriteFile(mdPath, []byte("# Session\nhello\n"), 0o644))

	_, w, err := NewWatcher(mdPath, 5*time.Millisecond)
	require.NoError(t, err)

	// Truncate the file.
	require.NoError(t, os.WriteFile(mdPath, []byte("# Session\n"), 0o644))

	msg := w.Next()
	_, ok := msg.(WatchTruncate)
	assert.True(t, ok, "expected WatchTruncate, got %T", msg)

	w.Close()
}

func TestWatcherClose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fsnotify behaviour differs on Windows")
	}

	tmp := t.TempDir()
	mdPath := filepath.Join(tmp, "session.md")

	require.NoError(t, os.WriteFile(mdPath, []byte("# Session\n"), 0o644))

	_, w, err := NewWatcher(mdPath, 5*time.Millisecond)
	require.NoError(t, err)

	// Close should not panic.
	err = w.Close()
	assert.NoError(t, err)

	// Second close should be safe.
	err = w.Close()
	assert.NoError(t, err)
}
