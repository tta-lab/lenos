package transcript

import (
	"errors"
	"log/slog"
	"os"
	"sync"
)

// ErrConcurrentWrite is returned when a second writer attempts to append while
// another holds the exclusive advisory lock.
var ErrConcurrentWrite = errors.New("transcript: concurrent .md writer detected")

// MdWriter appends to a .md file with per-write open/close and advisory flock
// for cross-process synchronization. Not safe for concurrent use from the same
// process; use a mutex or channel if needed.
type MdWriter struct {
	path string
	mu   sync.Mutex // serializes writes within this process
}

// NewMdWriter returns a writer for the given .md path. Does not open the file.
func NewMdWriter(path string) *MdWriter {
	return &MdWriter{path: path}
}

// Append opens the file with O_APPEND, acquires an exclusive advisory flock,
// writes p, fsyncs, unlocks, and closes. On EWOULDBLOCK (concurrent holder),
// returns ErrConcurrentWrite. On write error, logs to slog and returns nil
// (E8: render failure is non-halting for the agent loop).
func (w *MdWriter) Append(p []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		slog.Warn("transcript: OpenFile failed", "path", w.path, "err", err)
		return nil // E8: non-halting
	}
	defer f.Close()

	if err := w.flockExclusive(f); err != nil {
		return err // ErrConcurrentWrite
	}
	defer w.flockUnlock(f)

	if _, err := f.Write(p); err != nil {
		slog.Warn("transcript: write failed", "path", w.path, "err", err)
		return nil // E8: non-halting
	}

	if err := f.Sync(); err != nil {
		slog.Warn("transcript: fsync failed", "path", w.path, "err", err)
		// Non-fatal: Sync is best-effort
	}

	return nil
}
