package transcript

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMdWriter_AppendCreatesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "session.md")
	w := NewMdWriter(path)

	err := w.Append([]byte("hello world\n"))
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "hello world\n", string(data))
}

func TestMdWriter_AppendIsAppendOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "session.md")

	w := NewMdWriter(path)
	require.NoError(t, w.Append([]byte("first\n")))
	require.NoError(t, w.Append([]byte("second\n")))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "first\nsecond\n", string(data))
}

func TestMdWriter_ConcurrentWriteReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "session.md")

	// Create file and hold exclusive lock via a separate fd.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	require.NoError(t, err)
	defer f.Close()

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	require.NoError(t, err)
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	// Now try to write with MdWriter — should get ErrConcurrentWrite.
	w := NewMdWriter(path)
	err = w.Append([]byte("blocked\n"))
	require.ErrorIs(t, err, ErrConcurrentWrite)
}

// TestCrossProcessFlock verifies the cross-process advisory flock contract:
// MdWriter.Append MUST observe locks held on independent file descriptors,
// because cmd/log (Phase 3) opens its own fd to the same .md file. Since
// flock is per-fd (not per-process), a separate fd in this same process
// faithfully simulates a separate process — there is no semantic difference
// at the kernel level.
func TestCrossProcessFlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "session.md")

	// Goroutine simulates a second process holding the exclusive flock.
	locked := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			t.Errorf("helper open: %v", err)
			return
		}
		defer f.Close()
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Errorf("helper flock: %v", err)
			return
		}
		defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
		close(locked)
		<-release
	}()

	<-locked
	w := NewMdWriter(path)
	err := w.Append([]byte("blocked\n"))
	require.ErrorIs(t, err, ErrConcurrentWrite,
		"cross-process flock contract: separate fd holding flock must block MdWriter.Append")

	close(release)
	<-done

	// Lock released → next Append succeeds.
	require.NoError(t, w.Append([]byte("after\n")))
}

func TestMdWriter_WriteFailureNonHalting(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "readonly.md")

	// Create a read-only file — OpenFile will fail with EACCES on macOS.
	f, err := os.Create(path)
	require.NoError(t, err)
	f.Close()
	require.NoError(t, os.Chmod(path, 0o400))

	w := NewMdWriter(path)
	err = w.Append([]byte("won't be written\n"))
	// E8 contract: write failure returns nil (non-halting), logs warning.
	require.NoError(t, err)
}
