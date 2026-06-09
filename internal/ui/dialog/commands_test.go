package dialog

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/csync"
	"github.com/tta-lab/lenos/internal/ui/common"
	"github.com/tta-lab/lenos/internal/ui/styles"
	"github.com/tta-lab/lenos/internal/workspace"
)

type commandTestWorkspace struct {
	workspace.Workspace
	cfg        *config.Config
	workingDir string
}

func (w commandTestWorkspace) Config() *config.Config {
	return w.cfg
}

func (w commandTestWorkspace) WorkingDir() string {
	return w.workingDir
}

func TestDefaultCommandsIncludesBackgroundJobsForActiveSession(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	cmds, err := NewCommands(&common.Common{
		Workspace: commandTestWorkspace{
			cfg: &config.Config{
				Models:    map[config.SelectedModelType]config.SelectedModel{},
				Providers: csync.NewMap[string, config.ProviderConfig](),
				Agents:    map[string]config.Agent{},
			},
			workingDir: t.TempDir(),
		},
		Styles: &sty,
	}, "session-id", true, false, false, nil)
	require.NoError(t, err)

	var jobs *CommandItem
	for _, item := range cmds.defaultCommands() {
		if item.ID() == "background_jobs" {
			jobs = item
			break
		}
	}
	require.NotNil(t, jobs, "active sessions should expose a background jobs command")
	require.Equal(t, "Background Jobs", jobs.title)
	action, ok := jobs.Action().(ActionOpenDialog)
	require.True(t, ok, "background jobs command should open a dialog")
	require.Equal(t, BackgroundJobsID, action.DialogID)
}

func TestDefaultCommandsIncludesExportTrajectoryForActiveSession(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	cmds, err := NewCommands(&common.Common{
		Workspace: commandTestWorkspace{
			cfg: &config.Config{
				Options:   &config.Options{TUI: &config.TUIOptions{}},
				Models:    map[config.SelectedModelType]config.SelectedModel{},
				Providers: csync.NewMap[string, config.ProviderConfig](),
				Agents:    map[string]config.Agent{},
			},
			workingDir: t.TempDir(),
		},
		Styles: &sty,
	}, "session-1", true, false, false, nil)
	require.NoError(t, err)

	var export *CommandItem
	for _, item := range cmds.defaultCommands() {
		if item.ID() == "export_atif_trajectory" {
			export = item
			break
		}
	}
	require.NotNil(t, export, "active sessions should expose an ATIF export command")
	require.Equal(t, "Export ATIF Trajectory", export.title)
	_, ok := export.Action().(ActionExportTrajectory)
	require.True(t, ok, "export command should export the ATIF trajectory")
}

func TestDefaultCommandsIncludesNewSessionWithoutShortcut(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	cmds, err := NewCommands(&common.Common{
		Workspace: commandTestWorkspace{
			cfg: &config.Config{
				Options:   &config.Options{TUI: &config.TUIOptions{}},
				Models:    map[config.SelectedModelType]config.SelectedModel{},
				Providers: csync.NewMap[string, config.ProviderConfig](),
				Agents:    map[string]config.Agent{},
			},
			workingDir: t.TempDir(),
		},
		Styles: &sty,
	}, "session-1", true, false, false, nil)
	require.NoError(t, err)

	var newSession *CommandItem
	for _, item := range cmds.defaultCommands() {
		if item.ID() == "new_session" {
			newSession = item
			break
		}
	}
	require.NotNil(t, newSession, "command palette should expose new session")
	require.Equal(t, "New Session", newSession.title)
	require.Empty(t, newSession.Shortcut())
	_, ok := newSession.Action().(ActionNewSession)
	require.True(t, ok, "new session command should create a session")
}

func TestDefaultCommandsIncludesSingleSandboxToggle(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	cfg := &config.Config{
		Options:   &config.Options{TUI: &config.TUIOptions{}},
		Models:    map[config.SelectedModelType]config.SelectedModel{},
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Agents:    map[string]config.Agent{},
	}
	cmds, err := NewCommands(&common.Common{
		Workspace: commandTestWorkspace{
			cfg:        cfg,
			workingDir: t.TempDir(),
		},
		Styles: &sty,
	}, "session-id", true, false, false, nil)
	require.NoError(t, err)

	var sandboxItems []*CommandItem
	for _, item := range cmds.defaultCommands() {
		if item.ID() == "toggle_sandbox" {
			sandboxItems = append(sandboxItems, item)
		}
	}
	require.Len(t, sandboxItems, 1, "commands should expose exactly one sandbox toggle")
	require.Equal(t, "Disable Sandbox", sandboxItems[0].title)
	_, ok := sandboxItems[0].Action().(ActionToggleSandbox)
	require.True(t, ok, "sandbox command should toggle sandbox")

	off := false
	cfg.Options.Sandbox = &off
	var offItem *CommandItem
	for _, item := range cmds.defaultCommands() {
		if item.ID() == "toggle_sandbox" {
			offItem = item
			break
		}
	}
	require.NotNil(t, offItem)
	require.Equal(t, "Enable Sandbox", offItem.title)
}
