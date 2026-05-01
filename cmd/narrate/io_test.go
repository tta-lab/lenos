package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/transcript"
)

func flock(f *os.File, nb bool) {
	flags := syscall.LOCK_EX
	if nb {
		flags |= syscall.LOCK_NB
	}
	syscall.Flock(int(f.Fd()), flags)
}

func funlock(f *os.File) {
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

func TestResolveSessionPath(t *testing.T) {
	t.Run("missing SESSION_ID errors", func(t *testing.T) {
		t.Setenv("LENOS_SESSION_ID", "")
		_, err := resolveSessionPath()
		require.Error(t, err)
		require.Contains(t, err.Error(), "LENOS_SESSION_ID")
	})

	t.Run("plain cwd → <cwd>/.lenos", func(t *testing.T) {
		t.Setenv("LENOS_SESSION_ID", "abc123")
		dir := t.TempDir()
		t.Chdir(dir)

		path, err := resolveSessionPath()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(dir, ".lenos", "sessions", "abc123.md"), path)
	})

	t.Run("ancestor .lenos wins via LookupClosest", func(t *testing.T) {
		// /<tmp>/.lenos exists, cwd is /<tmp>/sub. narrate should resolve
		// the ancestor's .lenos, not create a new one in sub.
		t.Setenv("LENOS_SESSION_ID", "abc123")
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".lenos"), 0o755))
		sub := filepath.Join(root, "sub")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		t.Chdir(sub)

		path, err := resolveSessionPath()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(root, ".lenos", "sessions", "abc123.md"), path)
	})
}

func TestResolveInput(t *testing.T) {
	t.Run("args only", func(t *testing.T) {
		badReader := errorReader{}
		got, err := resolveInput([]string{"hello", "world"}, badReader)
		require.NoError(t, err)
		require.Equal(t, "hello world", got)
	})

	t.Run("stdin only", func(t *testing.T) {
		got, err := resolveInput([]string{}, strings.NewReader("piped content\n"))
		require.NoError(t, err)
		require.Equal(t, "piped content\n", got)
	})

	t.Run("both empty", func(t *testing.T) {
		_, err := resolveInput([]string{}, strings.NewReader(""))
		require.Error(t, err)
		require.Contains(t, err.Error(), "content required")
	})

	t.Run("args take precedence", func(t *testing.T) {
		badReader := errorReader{}
		got, err := resolveInput([]string{"args win"}, badReader)
		require.NoError(t, err)
		require.Equal(t, "args win", got)
	})
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("stdin should not be consumed")
}

func TestAppendWithRetry_Success(t *testing.T) {
	t.Parallel()

	t.Run("success first try", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "session.md")
		w := transcript.NewMdWriter(path)
		err := appendWithRetry(w, []byte("first\n"))
		require.NoError(t, err)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "first\n", string(data))
	})

	t.Run("success after retry", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "session.md")
		w := transcript.NewMdWriter(path)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		require.NoError(t, err)
		flock(f, true)
		started := make(chan struct{})
		go func() {
			close(started)
			time.Sleep(15 * time.Millisecond)
			funlock(f)
			f.Close()
		}()
		<-started
		err = appendWithRetry(w, []byte("after\n"))
		require.NoError(t, err)
	})

	t.Run("non retryable error returns immediately", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		roDir := filepath.Join(dir, "readonly")
		require.NoError(t, os.MkdirAll(roDir, 0o755))
		roPath := filepath.Join(roDir, "session.md")
		require.NoError(t, os.WriteFile(roPath, []byte(""), 0o644))
		require.NoError(t, os.Chmod(roDir, 0o400))
		defer os.Chmod(roDir, 0o755)
		rw := transcript.NewMdWriter(roPath)

		start := time.Now()
		err := appendWithRetry(rw, []byte("won't write\n"))
		elapsed := time.Since(start)

		require.Error(t, err)
		require.NotContains(t, err.Error(), "lock contention")
		require.Less(t, elapsed, 20*time.Millisecond, "non-retryable error should return immediately")
	})
}

// Exhaustion must NOT be parallel — the goroutine must hold the lock before
// appendWithRetry is called.
func TestAppendWithRetry_Exhaustion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.md")
	w := transcript.NewMdWriter(path)

	started := make(chan struct{})
	go func() {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			return
		}
		flock(f, true)
		close(started)
		time.Sleep(50 * time.Millisecond)
		funlock(f)
		f.Close()
	}()

	<-started
	err := appendWithRetry(w, []byte("blocked\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "lock contention")
}

func TestReadStdinIfPiped(t *testing.T) {
	t.Parallel()
	t.Run("pipes content through", func(t *testing.T) {
		t.Parallel()
		got, err := readStdinIfPiped(strings.NewReader("hello\nworld\n"))
		require.NoError(t, err)
		require.Equal(t, "hello\nworld\n", got)
	})
}
