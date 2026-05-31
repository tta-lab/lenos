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
// Renamed: CRUSH_SKIP_DATADIR_LOCK → LENOS_SKIP_DATADIR_LOCK

var (
	errLockFailed = errors.New("failed to acquire data directory lock")

	// SkipDataDirLock is an escape hatch for environments where flock is not
	// available or not desired. Set LENOS_SKIP_DATADIR_LOCK=true to bypass.
	SkipDataDirLock = os.Getenv("LENOS_SKIP_DATADIR_LOCK") == "true"
)

// lockDataDir acquires an advisory flock on the data directory to prevent
// multiple lenos instances from sharing the same SQLite database.
func lockDataDir(dataDir string) (*os.File, error) {
	if SkipDataDirLock {
		return nil, nil
	}

	lockPath := filepath.Join(dataDir, ".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to open lock file %s: %v", errLockFailed, lockPath, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf(
			"%w: could not acquire lock on %s (another lenos instance may be using this data directory; set LENOS_SKIP_DATADIR_LOCK=true to bypass): %v",
			errLockFailed, dataDir, err,
		)
	}

	// Write PID and process info so another instance can identify the owner.
	if _, err := fmt.Fprintf(f, "pid=%d\n", os.Getpid()); err != nil {
		slog.Debug("Failed to write lock metadata", "error", err)
	}
	if err := f.Sync(); err != nil {
		slog.Debug("Failed to sync lock metadata", "error", err)
	}

	return f, nil
}
