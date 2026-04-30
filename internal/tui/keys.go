package tui

import "charm.land/bubbles/v2/key"

// KeyMap holds key bindings for the viewport.
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
}

// DefaultKeyMap returns the standard key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Down:         key.NewBinding(key.WithKeys("down", "ctrl+j", "j"), key.WithHelp("↓/j", "down")),
		Up:           key.NewBinding(key.WithKeys("up", "ctrl+k", "k"), key.WithHelp("↑/k", "up")),
		HalfPageDown: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "half page down")),
		HalfPageUp:   key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "half page up")),
		PageDown:     key.NewBinding(key.WithKeys("pgdown", " ", "f"), key.WithHelp("f/pgdn/space", "page down")),
		PageUp:       key.NewBinding(key.WithKeys("pgup", "b"), key.WithHelp("b/pgup", "page up")),
		Home:         key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		End:          key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		Cancel:       key.NewBinding(key.WithKeys("esc", "alt+esc"), key.WithHelp("esc", "cancel")),
		Help:         key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("ctrl+g", "help")),
		Quit:         key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	}
}
