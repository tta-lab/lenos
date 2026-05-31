package dialog

import (
	"github.com/sahilm/fuzzy"
	"github.com/tta-lab/lenos/internal/agent"
	"github.com/tta-lab/lenos/internal/ui/list"
	"github.com/tta-lab/lenos/internal/ui/styles"
)

type BackgroundJobItem struct {
	sessionID string
	job       agent.BackgroundJob
	t         *styles.Styles
	m         fuzzy.Match
	cache     map[int]string
	focused   bool
}

var _ ListItem = &BackgroundJobItem{}

func backgroundJobItems(t *styles.Styles, sessionID string, jobs ...agent.BackgroundJob) []list.FilterableItem {
	items := make([]list.FilterableItem, len(jobs))
	for i, job := range jobs {
		items[i] = &BackgroundJobItem{
			sessionID: sessionID,
			job:       job,
			t:         t,
		}
	}
	return items
}

func (b *BackgroundJobItem) Filter() string {
	return b.job.Command + " " + b.job.ID
}

func (b *BackgroundJobItem) ID() string {
	return b.job.ID
}

func (b *BackgroundJobItem) SetFocused(focused bool) {
	if b.focused != focused {
		b.cache = nil
	}
	b.focused = focused
}

func (b *BackgroundJobItem) SetMatch(m fuzzy.Match) {
	b.cache = nil
	b.m = m
}

func (b *BackgroundJobItem) Render(width int) string {
	styles := ListItemStyles{
		ItemBlurred:     b.t.Dialog.NormalItem,
		ItemFocused:     b.t.Dialog.SelectedItem,
		InfoTextBlurred: b.t.Base,
		InfoTextFocused: b.t.Base,
	}
	return renderItem(styles, b.job.Command, b.job.ID, b.focused, width, b.cache, &b.m)
}
