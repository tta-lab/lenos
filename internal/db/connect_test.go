package db_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/db"
)

// TestConnectionPoolLimit verifies that SetMaxOpenConns(1) is in effect on
// the *sql.DB returned by Connect. This guards the port of upstream 61ee2d2e.
func TestConnectionPoolLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	conn, err := db.Connect(context.Background(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	stats := conn.Stats()
	require.Equal(t, 1, stats.MaxOpenConnections,
		"SetMaxOpenConns(1) must be in effect")
}

// TestPragmasVerify verifies that critical pragmas are set via the DSN or
// connection callback. These pragmas form the reliability surface that the
// upstream fixes protect.
func TestPragmasVerify(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	conn, err := db.Connect(context.Background(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	pragmas := map[string]string{
		"journal_mode":  "wal",
		"foreign_keys":  "1",
		"synchronous":   "1", // NORMAL = 1
		"secure_delete": "1",
		"busy_timeout":  "30000",
	}

	for name, expected := range pragmas {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var val string
			err := conn.QueryRowContext(context.Background(),
				fmt.Sprintf("PRAGMA %s", name)).Scan(&val)
			require.NoError(t, err)
			require.Equal(t, expected, strings.TrimSpace(val),
				"pragma %s should be %s", name, expected)
		})
	}
}

// TestMigrationUp verifies that goose.Up runs successfully on a fresh
// data directory. Exercises the full Connect path including migrations.
func TestMigrationUp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	conn, err := db.Connect(context.Background(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	require.NotNil(t, conn)

	// Verify the migrations actually created tables.
	rows, err := conn.QueryContext(context.Background(),
		"SELECT name FROM sqlite_master WHERE type='table' AND name IN ('sessions', 'messages', 'files')")
	require.NoError(t, err)
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.Len(t, tables, 3, "sessions, messages, and files tables must exist after migration")
}
