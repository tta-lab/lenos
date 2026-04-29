//go:build unix

package transcript

import (
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
		return err
	}
	return nil
}

// flockUnlock releases the advisory lock.
func (w *MdWriter) flockUnlock(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
