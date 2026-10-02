package ui

import "charm.land/bubbles/v2/key"

// keyMap collects every binding, as grid's does. The hints in the status bar
// are generated from the bindings themselves, so the two cannot disagree.
type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	HalfUp   key.Binding
	HalfDown key.Binding
	Top      key.Binding
	Bottom   key.Binding

	Open key.Binding
	Back key.Binding
	Tags key.Binding

	Find key.Binding
	// ClearFind is Back under the name it goes by while a find is set.
	ClearFind key.Binding
	Sort      key.Binding
	Reverse   key.Binding
	Refresh   key.Binding

	Quit key.Binding
	// Interrupt quits even while a prompt is open, where q is a letter.
	Interrupt key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up:       key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "up")),
		Down:     key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "space"), key.WithHelp("pgdn", "page down")),
		HalfUp:   key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "half a page up")),
		HalfDown: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "half a page down")),
		Top:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "first row")),
		Bottom:   key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "last row")),

		Open: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Back: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Tags: key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "tags")),

		Find:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "find tag")),
		ClearFind: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear")),
		Sort:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
		Reverse:   key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "reverse")),
		Refresh:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),

		Quit:      key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		Interrupt: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	}
}
