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
	cfg *config.Config
}

func (w commandTestWorkspace) Config() *config.Config {
	return w.cfg
}

func TestDefaultCommandsIncludesManualCompact(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	cmds, err := NewCommands(&common.Common{
		Workspace: commandTestWorkspace{cfg: &config.Config{
			Models:    map[config.SelectedModelType]config.SelectedModel{},
			Providers: csync.NewMap[string, config.ProviderConfig](),
			Agents:    map[string]config.Agent{},
		}},
		Styles: &sty,
	}, "session-id", true, false, false, nil)
	require.NoError(t, err)

	var compact *CommandItem
	for _, item := range cmds.defaultCommands() {
		if item.ID() == "compact" {
			compact = item
			break
		}
	}
	require.NotNil(t, compact, "active sessions should expose a manual compact command")
	require.Equal(t, "Compact Session", compact.title)
	_, ok := compact.Action().(ActionCompact)
	require.True(t, ok, "compact command should dispatch ActionCompact")
}

func TestDefaultCommandsIncludesBackgroundJobsForActiveSession(t *testing.T) {
	t.Parallel()

	sty := styles.DefaultStyles()
	cmds, err := NewCommands(&common.Common{
		Workspace: commandTestWorkspace{cfg: &config.Config{
			Models:    map[config.SelectedModelType]config.SelectedModel{},
			Providers: csync.NewMap[string, config.ProviderConfig](),
			Agents:    map[string]config.Agent{},
		}},
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
