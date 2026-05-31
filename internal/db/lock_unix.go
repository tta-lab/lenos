//go:build !windows

package db

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
)

// Ported from upstream commit 6923820a and 3c981c19.
// Original: feat(db): refuse to open a data directory in use by another crush
//
// 6923820a: advisory flock with PID metadata
// 3c981c19: log lock metadata write failures
//
// Adapted: uses LOCK_SH (shared) instead of LOCK_EX (exclusive).
// Multiple lenos processes share the same data directory concurrently;
// SQLite WAL mode with _txlock=immediate and busy_timeout=30000 handles
// cross-process write serialization. The shared lock exists only to
// prevent accidental concurrent migration runs (goose.Up) — goose
// has its own table-based locking, but the flock adds a safety net.
//
// Renamed: CRUSH_SKIP_DATADIR_LOCK → LENOS_SKIP_DATADIR_LOCK

var (
	errLockFailed = errors.New("failed to acquire data directory lock")

	// heldLockFile holds the lock file descriptor for the lifetime of
	// the process. flock is released when the process exits; closing
	// the fd then unlinking would create a race window on the inode.
	heldLockFile *os.File

	// SkipDataDirLock is an escape hatch. Set LENOS_SKIP_DATADIR_LOCK=true
	// to bypass the advisory flock entirely.
	SkipDataDirLock = os.Getenv("LENOS_SKIP_DATADIR_LOCK") == "true"
)

// lockDataDir acquires a shared advisory flock on the data directory.
// Returns an error if another process holds an exclusive lock (e.g.
// during migration). The lock is held for the lifetime of the process.
func lockDataDir(dataDir string) error {
	if SkipDataDirLock {
		return nil
	}

	lockPath := filepath.Join(dataDir, ".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("%w: failed to open lock file %s: %v", errLockFailed, lockPath, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		f.Close()
		return fmt.Errorf(
			"%w: could not acquire lock on %s (another lenos instance may be running migrations; set LENOS_SKIP_DATADIR_LOCK=true to bypass): %v",
			errLockFailed, dataDir, err,
		)
	}

	// Write PID so another instance can identify the holder.
	if _, err := fmt.Fprintf(f, "pid=%d\n", os.Getpid()); err != nil {
		slog.Debug("Failed to write lock metadata", "error", err)
	}
	if err := f.Sync(); err != nil {
		slog.Debug("Failed to sync lock metadata", "error", err)
	}

	// Hold the reference so the fd stays alive. On process exit the
	// kernel releases the flock automatically.
	heldLockFile = f
	return nil
}
