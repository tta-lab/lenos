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
// the *sql.DB returned by Connect. This is the port of upstream 61ee2d2e.
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

// TestConcurrentAccess verifies that two goroutines can open connections
// and perform writes concurrently without SQLITE_BUSY errors. This
// exercises the combination of _txlock=immediate + busy_timeout=30000
// that the upstream fixes depend on.
func TestConcurrentAccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	conn1, err := db.Connect(context.Background(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { conn1.Close() })

	// Create a test table.
	_, err = conn1.ExecContext(context.Background(),
		"CREATE TABLE IF NOT EXISTS concurrent_test (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)

	errCh := make(chan error, 2)

	go func() {
		conn, err := db.Connect(context.Background(), dir)
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()
		_, err = conn.ExecContext(context.Background(),
			"INSERT INTO concurrent_test (id) VALUES (1)")
		errCh <- err
	}()

	go func() {
		conn, err := db.Connect(context.Background(), dir)
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()
		_, err = conn.ExecContext(context.Background(),
			"INSERT INTO concurrent_test (id) VALUES (2)")
		errCh <- err
	}()

	for i := 0; i < 2; i++ {
		require.NoError(t, <-errCh,
			"concurrent writers must not fail with SQLITE_BUSY")
	}
}

// TestTempStoreMemory verifies that temp_store is set to MEMORY at the
// connection level. This is the port of upstream 56cf50ad.
func TestTempStoreMemory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	conn, err := db.Connect(context.Background(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	var tempStore string
	err = conn.QueryRowContext(context.Background(), "PRAGMA temp_store").Scan(&tempStore)
	require.NoError(t, err)
	require.Equal(t, "2", strings.TrimSpace(tempStore),
		"PRAGMA temp_store should be 2 (MEMORY)")
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
	conn.Close()
}

// TestConnectRejectsEmptyDir verifies that Connect returns an error when
// dataDir is empty.
func TestConnectRejectsEmptyDir(t *testing.T) {
	t.Parallel()
	conn, err := db.Connect(context.Background(), "")
	require.Error(t, err)
	require.Nil(t, conn)
	require.Contains(t, err.Error(), "data.dir")
}
