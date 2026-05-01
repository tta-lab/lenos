package tui

import "charm.land/bubbles/v2/key"

// KeyMap holds key bindings for the App. It implements help.KeyMap so the
// ctrl+g overlay can render short + full help views.
type KeyMap struct {
	Down         key.Binding
	Up           key.Binding
	HalfPageDown key.Binding
	HalfPageUp   key.Binding
	PageDown     key.Binding
	PageUp       key.Binding
	Home         key.Binding
	End          key.Binding
	Cancel       key.Binding
	Help         key.Binding
	Quit         key.Binding
	Retry        key.Binding
	HeaderToggle key.Binding
	BottomToggle key.Binding
	Submit       key.Binding
}

// DefaultKeyMap returns the standard key bindings with their help text
// fully populated for the ctrl+g overlay.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Down:         key.NewBinding(key.WithKeys("down", "ctrl+j", "j"), key.WithHelp("↓/j", "scroll down")),
		Up:           key.NewBinding(key.WithKeys("up", "ctrl+k", "k"), key.WithHelp("↑/k", "scroll up")),
		HalfPageDown: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "half page down")),
		HalfPageUp:   key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "half page up")),
		PageDown:     key.NewBinding(key.WithKeys("pgdown", " ", "f"), key.WithHelp("f/␣/pgdn", "page down")),
		PageUp:       key.NewBinding(key.WithKeys("pgup", "b"), key.WithHelp("b/pgup", "page up")),
		Home:         key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "jump to top")),
		End:          key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "jump to bottom")),
		Cancel:       key.NewBinding(key.WithKeys("esc", "alt+esc"), key.WithHelp("esc", "cancel")),
		Help:         key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("ctrl+g", "toggle help")),
		Quit:         key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		Retry:        key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "retry watcher")),
		HeaderToggle: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "toggle header")),
		BottomToggle: key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "toggle queue panel")),
		Submit:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
	}
}

// ShortHelp implements help.KeyMap. The few keys most users reach for first.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.HeaderToggle, k.BottomToggle, k.Help, k.Quit}
}

// FullHelp implements help.KeyMap, grouped by column.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Submit, k.HeaderToggle, k.BottomToggle},
		{k.Down, k.Up, k.HalfPageDown, k.HalfPageUp, k.PageDown, k.PageUp},
		{k.Home, k.End, k.Retry, k.Help, k.Quit},
	}
}
