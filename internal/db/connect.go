package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/pressly/goose/v3"
)

var pragmas = map[string]string{
	"foreign_keys":  "ON",
	"journal_mode":  "WAL",
	"page_size":     "4096",
	"cache_size":    "-8000",
	"synchronous":   "NORMAL",
	"secure_delete": "ON",
	"busy_timeout":  "30000",
}

// gooseMu serializes goose global state operations to avoid data races in the
// goose library when tests run in parallel with -race. goose uses package-level
// globals for table creation and dialect state.
var gooseMu sync.Mutex

// gooseInitMu ensures goose is only initialized once.
var gooseInitMu sync.Once

// Connect opens a SQLite database connection and runs migrations.
func Connect(ctx context.Context, dataDir string) (*sql.DB, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("data.dir is not set")
	}
	dbPath := filepath.Join(dataDir, "lenos.db")

	db, err := openDB(dbPath)
	if err != nil {
		return nil, err
	}

	// Ported from upstream commit 61ee2d2e.
	// Original: fix(db): use connection pool to avoid corrupted writes
	db.SetMaxOpenConns(1)

	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Adapted from upstream commit 6923820a.
	// Original: feat(db): refuse to open a data directory in use by another crush
	//
	// Uses LOCK_SH (shared) — multiple lenos processes can share the
	// database concurrently. SQLite WAL mode with _txlock=immediate
	// and busy_timeout handles cross-process write serialization.
	// The shared lock is a safety net against concurrent goose.Up()
	// migration runs.
	//
	// Renamed: CRUSH_SKIP_DATADIR_LOCK → LENOS_SKIP_DATADIR_LOCK
	if err := lockDataDir(dataDir); err != nil {
		db.Close()
		return nil, err
	}

	gooseMu.Lock()
	defer gooseMu.Unlock()

	gooseInitMu.Do(func() {
		goose.SetBaseFS(FS)
	})

	if err := goose.SetDialect("sqlite3"); err != nil {
		slog.Error("Failed to set dialect", "error", err)
		return nil, fmt.Errorf("failed to set dialect: %w", err)
	}

	if err := goose.Up(db, "migrations"); err != nil {
		slog.Error("Failed to apply migrations", "error", err)
		return nil, fmt.Errorf("failed to apply migrations: %w", err)
	}

	return db, nil
}
