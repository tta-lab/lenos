package dialog

import (
	"context"
	"fmt"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/tta-lab/lenos/internal/agent"
	"github.com/tta-lab/lenos/internal/ui/common"
	"github.com/tta-lab/lenos/internal/ui/list"
	"github.com/tta-lab/lenos/internal/ui/util"
)

const BackgroundJobsID = "background_jobs"

type BackgroundJobs struct {
	com       *common.Common
	sessionID string
	jobs      []agent.BackgroundJob

	help  help.Model
	input textinput.Model
	list  *list.FilterableList

	keyMap struct {
		Kill     key.Binding
		Next     key.Binding
		Previous key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*BackgroundJobs)(nil)

func NewBackgroundJobs(com *common.Common, sessionID string) (*BackgroundJobs, error) {
	b := &BackgroundJobs{
		com:       com,
		sessionID: sessionID,
		jobs:      com.Workspace.AgentActiveBackgroundJobs(sessionID),
	}
	b.help = help.New()
	b.help.Styles = com.Styles.DialogHelpStyles()

	items := backgroundJobItems(com.Styles, sessionID, b.jobs...)
	if len(items) == 0 {
		items = append(items, NewCommandItem(com.Styles, "no_background_jobs", "No active background jobs", "", nil))
	}
	b.list = list.NewFilterableList(items...)
	b.list.Focus()
	b.list.SetSelected(0)

	b.input = textinput.New()
	b.input.SetVirtualCursor(false)
	b.input.Placeholder = "Type to filter"
	b.input.SetStyles(com.Styles.TextInput)
	b.input.Focus()

	b.keyMap.Kill = key.NewBinding(
		key.WithKeys("enter", "ctrl+x"),
		key.WithHelp("enter", "kill"),
	)
	b.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next item"),
	)
	b.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous item"),
	)
	b.keyMap.Close = CloseKey
	return b, nil
}

func (b *BackgroundJobs) ID() string {
	return BackgroundJobsID
}

func (b *BackgroundJobs) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, b.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, b.keyMap.Previous):
			b.list.Focus()
			if b.list.IsSelectedFirst() {
				b.list.SelectLast()
			} else {
				b.list.SelectPrev()
			}
			b.list.ScrollToSelected()
		case key.Matches(msg, b.keyMap.Next):
			b.list.Focus()
			if b.list.IsSelectedLast() {
				b.list.SelectFirst()
			} else {
				b.list.SelectNext()
			}
			b.list.ScrollToSelected()
		case key.Matches(msg, b.keyMap.Kill):
			if selectedItem := b.list.SelectedItem(); selectedItem != nil {
				if item, ok := selectedItem.(*BackgroundJobItem); ok && item != nil {
					return ActionKillBackgroundJob{SessionID: item.sessionID, JobID: item.job.ID}
				}
			}
		default:
			var cmd tea.Cmd
			b.input, cmd = b.input.Update(msg)
			b.list.SetFilter(b.input.Value())
			b.list.ScrollToTop()
			b.list.SetSelected(0)
			return ActionCmd{cmd}
		}
	}
	return nil
}

func (b *BackgroundJobs) Cursor() *tea.Cursor {
	return InputCursor(b.com.Styles, b.input.Cursor())
}

func (b *BackgroundJobs) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := b.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()

	b.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1))
	b.list.SetSize(innerWidth, height-heightOffset)
	b.help.SetWidth(innerWidth)

	rc := NewRenderContext(t, width)
	rc.Title = "Background Jobs"
	rc.AddPart(t.Dialog.InputPrompt.Render(b.input.View()))
	rc.AddPart(t.Dialog.List.Height(b.list.Height()).Render(b.list.Render()))
	rc.Help = b.help.View(b)

	view := rc.Render()
	cur := b.Cursor()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

func (b *BackgroundJobs) ShortHelp() []key.Binding {
	return []key.Binding{
		b.keyMap.Kill,
		b.keyMap.Next,
		b.keyMap.Previous,
		b.keyMap.Close,
	}
}

func (b *BackgroundJobs) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{b.keyMap.Kill, b.keyMap.Next, b.keyMap.Previous},
		{b.keyMap.Close},
	}
}

func KillBackgroundJobCmd(com *common.Common, sessionID, jobID string) tea.Cmd {
	return func() tea.Msg {
		if err := com.Workspace.AgentKillBackgroundJob(context.Background(), sessionID, jobID); err != nil {
			return util.ReportError(err)()
		}
		return util.NewInfoMsg(fmt.Sprintf("Background job killed: %s", jobID))
	}
}
