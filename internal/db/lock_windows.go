//go:build windows

package db

// lockDataDir is a no-op on Windows. Advisory file locking via flock is not
// available; Windows processes share the SQLite database through WAL mode
// with _txlock=immediate handling write serialization.
func lockDataDir(dataDir string) error {
	return nil
}
