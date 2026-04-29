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
