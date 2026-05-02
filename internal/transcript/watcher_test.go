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

// nextWithTimeout pulls the next event from the watcher or fails the test
// instead of blocking forever — guards CI runs on slow filesystems (Docker
// overlay, NFS, loaded runners) where fsnotify events can arrive late or
// not at all.
func nextWithTimeout(t *testing.T, w *Watcher, timeout time.Duration) WatchEvent {
	t.Helper()
	done := make(chan WatchEvent, 1)
	go func() { done <- w.Next() }()
	select {
	case ev := <-done:
		return ev
	case <-time.After(timeout):
		t.Fatalf("watcher event did not arrive within %s", timeout)
		return nil
	}
}

const watcherTestTimeout = 5 * time.Second

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

	msg := nextWithTimeout(t, w, watcherTestTimeout)
	appendMsg, ok := msg.(WatchAppend)
	require.True(t, ok, "expected WatchAppend, got %T", msg)
	assert.Equal(t, []byte("\n```bash\ngo build\n```\n"), appendMsg.Bytes)

	// Multiple writes within debounce window coalesce.
	require.NoError(t, os.WriteFile(mdPath, []byte("# Session\n\n```bash\ngo build\n```\n\noutput\n"), 0o644))
	require.NoError(t, os.WriteFile(mdPath, []byte("# Session\n\n```bash\ngo build\n```\n\noutput\nerror\n"), 0o644))

	msg = nextWithTimeout(t, w, watcherTestTimeout)
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

	msg := nextWithTimeout(t, w, watcherTestTimeout)
	tr, ok := msg.(WatchTruncate)
	assert.True(t, ok, "expected WatchTruncate, got %T", msg)
	// WatchTruncate is contractually empty — the read-back happens in the UI
	// layer (see attachMdView's truncation branch). Pin the empty-payload
	// contract so a future watcher change can't silently start passing bytes
	// that callers would ignore (or worse, accidentally trust).
	assert.Equal(t, WatchTruncate{}, tr, "WatchTruncate must carry no fields — caller re-reads the file")

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
