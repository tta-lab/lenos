package transcript

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
)

// ErrConcurrentWrite is returned when a second writer attempts to append while
// another holds the exclusive advisory lock.
var ErrConcurrentWrite = errors.New("transcript: concurrent .md writer detected")

// MdWriter appends to a .md file with per-write open/close and advisory flock
// for cross-process synchronization. The mutex serializes calls within a single
// process; flock serializes across processes.
//
// Two error policies:
//
//   - AppendStrict returns all I/O errors honestly. Callers that must not lose
//     errors (e.g. cmd/narrate, E14) use this.
//   - Append calls AppendStrict and applies E8: non-ErrConcurrentWrite errors are
//     logged via slog.Warn and returned as nil. Lenos main uses Append.
type MdWriter struct {
	path string
	mu   sync.Mutex // serializes writes within this process
}

// NewMdWriter returns a writer for the given .md path. Does not open the file.
func NewMdWriter(path string) *MdWriter {
	return &MdWriter{path: path}
}

// AppendStrict opens the file with O_APPEND, acquires an exclusive advisory flock,
// writes p, fsyncs, unlocks, and closes. Returns all errors honestly:
// os.OpenFile failure, ErrConcurrentWrite, write error, fsync error. Callers
// that must not silently swallow errors use this (e.g. cmd/narrate, E14).
func (w *MdWriter) AppendStrict(p []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", w.path, err)
	}
	defer f.Close()

	if err := w.flockExclusive(f); err != nil {
		return err // ErrConcurrentWrite
	}
	defer w.flockUnlock(f)

	if _, err := f.Write(p); err != nil {
		return fmt.Errorf("write %s: %w", w.path, err)
	}

	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync %s: %w", w.path, err)
	}

	return nil
}

// Append calls AppendStrict and applies E8: non-ErrConcurrentWrite errors are
// logged via slog.Warn and returned as nil. Lenos main uses this for the
// non-halting render contract.
func (w *MdWriter) Append(p []byte) error {
	err := w.AppendStrict(p)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrConcurrentWrite) {
		return err
	}
	slog.Warn("transcript: append failed (non-halting)", "path", w.path, "err", err)
	return nil
}
