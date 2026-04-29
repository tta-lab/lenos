//go:build windows

package transcript

import "os"

// flockExclusive is a no-op on Windows; advisory flock is not supported.
// Concurrent writes from multiple processes are not detected on this platform.
func (w *MdWriter) flockExclusive(f *os.File) error {
	return nil
}

// flockUnlock is a no-op on Windows.
func (w *MdWriter) flockUnlock(f *os.File) {}
