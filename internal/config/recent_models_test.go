package config

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// readConfigJSON reads and unmarshals the JSON config file at path.
func readConfigJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	baseDir := filepath.Dir(path)
	fileName := filepath.Base(path)
	b, err := fs.ReadFile(os.DirFS(baseDir), fileName)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(b, &out))
	return out
}

// readRecentModels reads the recent_models section from the config file.
func readRecentModels(t *testing.T, path string) []any {
	t.Helper()
	out := readConfigJSON(t, path)
	rm, ok := out["recent_models"].([]any)
	require.True(t, ok)
	return rm
}

// testStoreWithPath creates a ConfigStore backed by a Config for recent model tests.
func testStoreWithPath(cfg *Config, dir string) *ConfigStore {
	return &ConfigStore{
		config:         cfg,
		globalDataPath: filepath.Join(dir, "config.json"),
	}
}

func TestRecordRecentModel_AddsAndPersists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := &Config{}
	cfg.setDefaults(dir, "")
	store := testStoreWithPath(cfg, dir)

	err := store.recordRecentModel(ScopeGlobal, SelectedModel{Provider: "openai", Model: "gpt-4o"})
	require.NoError(t, err)

	// in-memory state
	require.Len(t, cfg.RecentModels, 1)
	require.Equal(t, "openai", cfg.RecentModels[0].Provider)
	require.Equal(t, "gpt-4o", cfg.RecentModels[0].Model)

	// persisted state
	rm := readRecentModels(t, store.globalDataPath)
	require.Len(t, rm, 1)
	item, ok := rm[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "openai", item["provider"])
	require.Equal(t, "gpt-4o", item["model"])
}

func TestRecordRecentModel_DedupeAndMoveToFront(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := &Config{}
	cfg.setDefaults(dir, "")
	store := testStoreWithPath(cfg, dir)

	// Add two entries
	require.NoError(t, store.recordRecentModel(ScopeGlobal, SelectedModel{Provider: "openai", Model: "gpt-4o"}))
	require.NoError(t, store.recordRecentModel(ScopeGlobal, SelectedModel{Provider: "anthropic", Model: "claude"}))
	// Re-add first; should move to front and not duplicate
	require.NoError(t, store.recordRecentModel(ScopeGlobal, SelectedModel{Provider: "openai", Model: "gpt-4o"}))

	got := cfg.RecentModels
	require.Len(t, got, 2)
	require.Equal(t, SelectedModel{Provider: "openai", Model: "gpt-4o"}, got[0])
	require.Equal(t, SelectedModel{Provider: "anthropic", Model: "claude"}, got[1])
}

func TestRecordRecentModel_TrimsToMax(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := &Config{}
	cfg.setDefaults(dir, "")
	store := testStoreWithPath(cfg, dir)

	// Insert 11 unique models; max is 10
	entries := []SelectedModel{
		{Provider: "p1", Model: "m1"},
		{Provider: "p2", Model: "m2"},
		{Provider: "p3", Model: "m3"},
		{Provider: "p4", Model: "m4"},
		{Provider: "p5", Model: "m5"},
		{Provider: "p6", Model: "m6"},
		{Provider: "p7", Model: "m7"},
		{Provider: "p8", Model: "m8"},
		{Provider: "p9", Model: "m9"},
		{Provider: "p10", Model: "m10"},
		{Provider: "p11", Model: "m11"},
	}
	for _, e := range entries {
		require.NoError(t, store.recordRecentModel(ScopeGlobal, e))
	}

	// in-memory state
	got := cfg.RecentModels
	require.Len(t, got, 10)
	// Newest first, capped at 10: p11..p2
	require.Equal(t, SelectedModel{Provider: "p11", Model: "m11"}, got[0])
	require.Equal(t, SelectedModel{Provider: "p10", Model: "m10"}, got[1])
	require.Equal(t, SelectedModel{Provider: "p9", Model: "m9"}, got[2])
	require.Equal(t, SelectedModel{Provider: "p8", Model: "m8"}, got[3])
	require.Equal(t, SelectedModel{Provider: "p7", Model: "m7"}, got[4])
	require.Equal(t, SelectedModel{Provider: "p6", Model: "m6"}, got[5])
	require.Equal(t, SelectedModel{Provider: "p5", Model: "m5"}, got[6])
	require.Equal(t, SelectedModel{Provider: "p4", Model: "m4"}, got[7])
	require.Equal(t, SelectedModel{Provider: "p3", Model: "m3"}, got[8])
	require.Equal(t, SelectedModel{Provider: "p2", Model: "m2"}, got[9])

	// persisted state: verify trimmed to 10 and newest-first order
	rm := readRecentModels(t, store.globalDataPath)
	require.Len(t, rm, 10)
	// Build provider:model IDs and verify order
	var ids []string
	for _, v := range rm {
		m := v.(map[string]any)
		ids = append(ids, m["provider"].(string)+":"+m["model"].(string))
	}
	require.Equal(t, []string{"p11:m11", "p10:m10", "p9:m9", "p8:m8", "p7:m7", "p6:m6", "p5:m5", "p4:m4", "p3:m3", "p2:m2"}, ids)
}

func TestRecordRecentModel_SkipsEmptyValues(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := &Config{}
	cfg.setDefaults(dir, "")
	store := testStoreWithPath(cfg, dir)

	// Missing provider
	require.NoError(t, store.recordRecentModel(ScopeGlobal, SelectedModel{Provider: "", Model: "m"}))
	// Missing model
	require.NoError(t, store.recordRecentModel(ScopeGlobal, SelectedModel{Provider: "p", Model: ""}))

	require.Len(t, cfg.RecentModels, 0)
	// No file should be written (stat via fs.FS)
	baseDir := filepath.Dir(store.globalDataPath)
	fileName := filepath.Base(store.globalDataPath)
	_, err := fs.Stat(os.DirFS(baseDir), fileName)
	require.True(t, os.IsNotExist(err))
}

func TestRecordRecentModel_NoPersistOnNoop(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := &Config{}
	cfg.setDefaults(dir, "")
	store := testStoreWithPath(cfg, dir)

	entry := SelectedModel{Provider: "openai", Model: "gpt-4o"}
	require.NoError(t, store.recordRecentModel(ScopeGlobal, entry))

	baseDir := filepath.Dir(store.globalDataPath)
	fileName := filepath.Base(store.globalDataPath)
	before, err := fs.ReadFile(os.DirFS(baseDir), fileName)
	require.NoError(t, err)

	// Get file ModTime to verify no write occurs
	stBefore, err := fs.Stat(os.DirFS(baseDir), fileName)
	require.NoError(t, err)
	beforeMod := stBefore.ModTime()

	// Re-record same entry should be a no-op (no write)
	require.NoError(t, store.recordRecentModel(ScopeGlobal, entry))

	after, err := fs.ReadFile(os.DirFS(baseDir), fileName)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))

	// Verify ModTime unchanged to ensure truly no write occurred
	stAfter, err := fs.Stat(os.DirFS(baseDir), fileName)
	require.NoError(t, err)
	require.True(t, stAfter.ModTime().Equal(beforeMod), "file ModTime should not change on noop")
}

func TestUpdatePreferredModel_UpdatesRecents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := &Config{}
	cfg.setDefaults(dir, "")
	store := testStoreWithPath(cfg, dir)

	sel := SelectedModel{Provider: "openai", Model: "gpt-4o"}
	require.NoError(t, store.UpdatePreferredModel(ScopeGlobal, sel))

	// in-memory
	require.NotNil(t, cfg.Model)
	require.Equal(t, sel, *cfg.Model)
	require.Len(t, cfg.RecentModels, 1)

	// persisted (read via fs.FS)
	rm := readRecentModels(t, store.globalDataPath)
	require.Len(t, rm, 1)
}

func TestRecordRecentModel_Ordering(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := &Config{}
	cfg.setDefaults(dir, "")
	store := testStoreWithPath(cfg, dir)

	// Add models and verify newest-first ordering
	model1 := SelectedModel{Provider: "openai", Model: "gpt-4o"}
	model2 := SelectedModel{Provider: "anthropic", Model: "claude"}

	require.NoError(t, store.recordRecentModel(ScopeGlobal, model1))
	require.NoError(t, store.recordRecentModel(ScopeGlobal, model2))

	require.Len(t, cfg.RecentModels, 2)
	require.Equal(t, model2, cfg.RecentModels[0])
	require.Equal(t, model1, cfg.RecentModels[1])
}
