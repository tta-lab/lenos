package config

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigStore_ConfigPath_GlobalAlwaysWorks(t *testing.T) {
	t.Parallel()

	store := &ConfigStore{
		globalDataPath: "/some/global/config.json",
	}

	path, err := store.configPath(ScopeGlobal)
	require.NoError(t, err)
	require.Equal(t, "/some/global/config.json", path)
}

func TestConfigStore_ConfigPath_WorkspaceReturnsPath(t *testing.T) {
	t.Parallel()

	store := &ConfigStore{
		workspacePath: "/some/workspace/.lenos/config.json",
	}

	path, err := store.configPath(ScopeWorkspace)
	require.NoError(t, err)
	require.Equal(t, "/some/workspace/.lenos/config.json", path)
}

func TestConfigStore_ConfigPath_WorkspaceErrorsWhenEmpty(t *testing.T) {
	t.Parallel()

	store := &ConfigStore{
		globalDataPath: "/some/global/config.json",
		workspacePath:  "",
	}

	_, err := store.configPath(ScopeWorkspace)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoWorkspaceConfig))
}

func TestConfigStore_SetConfigField_WorkspaceScopeGuard(t *testing.T) {
	t.Parallel()

	store := &ConfigStore{
		config:         &Config{},
		globalDataPath: filepath.Join(t.TempDir(), "global.json"),
		workspacePath:  "",
	}

	err := store.SetConfigField(ScopeWorkspace, "foo", "bar")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoWorkspaceConfig))
}

func TestConfigStore_SetConfigField_GlobalScopeAlwaysWorks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	globalPath := filepath.Join(dir, "config.json")
	store := &ConfigStore{
		config:         &Config{},
		globalDataPath: globalPath,
	}

	err := store.SetConfigField(ScopeGlobal, "foo", "bar")
	require.NoError(t, err)

	data, err := os.ReadFile(globalPath)
	require.NoError(t, err)
	require.Contains(t, string(data), `"foo"`)
}

func TestConfigStore_RemoveConfigField_WorkspaceScopeGuard(t *testing.T) {
	t.Parallel()

	store := &ConfigStore{
		config:         &Config{},
		globalDataPath: filepath.Join(t.TempDir(), "global.json"),
		workspacePath:  "",
	}

	err := store.RemoveConfigField(ScopeWorkspace, "foo")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoWorkspaceConfig))
}

func TestConfigStore_HasConfigField_WorkspaceScopeGuard(t *testing.T) {
	t.Parallel()

	store := &ConfigStore{
		config:         &Config{},
		globalDataPath: filepath.Join(t.TempDir(), "global.json"),
		workspacePath:  "",
	}

	has := store.HasConfigField(ScopeWorkspace, "foo")
	require.False(t, has)
}

func TestGlobalWorkspaceDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LENOS_GLOBAL_DATA", dir)

	wsDir := GlobalWorkspaceDir()
	globalData := GlobalConfigData()

	require.Equal(t, filepath.Dir(globalData), wsDir)
	require.Equal(t, dir, wsDir)
}

func TestScope_String(t *testing.T) {
	t.Parallel()

	require.Equal(t, "global", ScopeGlobal.String())
	require.Equal(t, "workspace", ScopeWorkspace.String())
	require.Contains(t, Scope(99).String(), "Scope(99)")
}

func TestConfigStore_SetActiveModel_DoesNotWriteConfigFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"existing":"data"}`), 0o600))
	statBefore, _ := os.Stat(path)
	contentBefore, _ := os.ReadFile(path)

	store := &ConfigStore{
		config:         &Config{Models: make(map[SelectedModelType]SelectedModel)},
		globalDataPath: path,
	}
	store.SetActiveModel(SelectedModelTypeLarge, SelectedModel{Provider: "openai", Model: "gpt-4o"})

	statAfter, _ := os.Stat(path)
	contentAfter, _ := os.ReadFile(path)
	require.Equal(t, statBefore.ModTime(), statAfter.ModTime(), "mtime must not change")
	require.Equal(t, sha256.Sum256(contentBefore), sha256.Sum256(contentAfter), "content must not change")
}

func TestConfigStore_SetActiveModel_DoesNotRecordRecents(t *testing.T) {
	t.Parallel()

	existing := []SelectedModel{{Provider: "anthropic", Model: "claude-3-opus"}}
	store := &ConfigStore{
		config: &Config{
			Models:       map[SelectedModelType]SelectedModel{},
			RecentModels: map[SelectedModelType][]SelectedModel{SelectedModelTypeLarge: existing},
		},
	}
	store.SetActiveModel(SelectedModelTypeLarge, SelectedModel{Provider: "openai", Model: "gpt-4o"})
	require.Equal(t, existing, store.config.RecentModels[SelectedModelTypeLarge], "recents must be byte-identical")
}

func TestConfigStore_SetActiveModel_UpdatesInMemoryOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"models":{"large":{"provider":"old","model":"old-model"}}}`), 0o600))

	store := &ConfigStore{
		config:         &Config{Models: map[SelectedModelType]SelectedModel{SelectedModelTypeLarge: {Provider: "old", Model: "old-model"}}},
		globalDataPath: path,
	}
	newModel := SelectedModel{Provider: "openai", Model: "gpt-4o"}
	store.SetActiveModel(SelectedModelTypeLarge, newModel)

	// In-memory updated.
	require.Equal(t, newModel, store.config.Models[SelectedModelTypeLarge])

	// File still shows old.
	data, _ := os.ReadFile(path)
	require.Contains(t, string(data), "old-model")
	require.NotContains(t, string(data), "gpt-4o")
}
