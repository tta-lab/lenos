//go:build (darwin && (amd64 || arm64)) || (freebsd && (amd64 || arm64)) || (linux && (386 || amd64 || arm || arm64 || loong64 || ppc64le || riscv64 || s390x)) || (windows && (386 || amd64 || arm64))

package db

import (
	"database/sql"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite"
)

func openDB(dbPath string) (*sql.DB, error) {
	// Set pragmas for better performance via _pragma query params.
	// Format: _pragma=name(value)
	params := url.Values{}
	for name, value := range pragmas {
		params.Add("_pragma", fmt.Sprintf("%s(%s)", name, value))
	}
	// Ported from upstream commit 40108413.
	// Original: fix(db): prevent SQLITE_NOTADB corruption under concurrent sub-agents
	params.Add("_txlock", "immediate")
	// Ported from upstream commit 56cf50ad.
	// Original: fix(db): keep SQLite temp files in memory
	params.Add("_pragma", "temp_store(MEMORY)")

	dsn := fmt.Sprintf("file:%s?%s", dbPath, params.Encode())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return db, nil
}
