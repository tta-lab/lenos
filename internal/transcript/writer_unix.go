//go:build unix

package transcript

import (
	"log/slog"
	"os"
	"syscall"
)

// flockExclusive acquires an exclusive advisory lock on fd using flock(2).
// Returns ErrConcurrentWrite if the lock is already held by another process.
func (w *MdWriter) flockExclusive(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
			return ErrConcurrentWrite
		}
		slog.Error("transcript: flock exclusive failed", "path", w.path, "err", err)
		return err
	}
	return nil
}

// flockUnlock releases the advisory lock. Errors are logged but not returned
// since the file is always closed immediately after.
func (w *MdWriter) flockUnlock(f *os.File) {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		slog.Warn("transcript: flock unlock failed", "path", w.path, "err", err)
	}
}
