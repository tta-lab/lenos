//go:build windows

package db

import "os"

// lockDataDir is a no-op on Windows. Advisory file locking via flock is not
// available; Windows relies on exclusive file-mode to prevent concurrent
// access.
func lockDataDir(dataDir string) (*os.File, error) {
	return nil, nil
}
