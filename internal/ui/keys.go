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
	// Tab switches between the events and the markets of a tag.
	Tab key.Binding

	Find key.Binding
	// ClearFind is Back under the name it goes by while a find is set.
	ClearFind key.Binding
	// Search is Find's key in a list of events or markets, where the text
	// goes to the service rather than narrowing the rows in hand.
	Search  key.Binding
	Filter  key.Binding
	SubTag  key.Binding
	Sort    key.Binding
	Reverse key.Binding
	Refresh key.Binding

	// The keys of the filter form.
	Next     key.Binding
	Previous key.Binding
	Left     key.Binding
	Right    key.Binding
	Apply    key.Binding
	Cancel   key.Binding

	// The keys of a market's detail.
	Interval key.Binding
	// About is Tab's key on a market, where it shows the description.
	About  key.Binding
	Browse key.Binding
	Copy   key.Binding
	CopyID key.Binding

	Help key.Binding

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
		Tab:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "events / markets")),

		Find:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "find tag")),
		ClearFind: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear")),
		Search:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Filter:    key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filter")),
		SubTag:    key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "sub-tag")),
		Sort:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
		Reverse:   key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "reverse")),
		Refresh:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),

		Next:     key.NewBinding(key.WithKeys("tab", "down"), key.WithHelp("tab", "next field")),
		Previous: key.NewBinding(key.WithKeys("shift+tab", "up"), key.WithHelp("shift+tab", "previous field")),
		Left:     key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "previous choice")),
		Right:    key.NewBinding(key.WithKeys("right", "space"), key.WithHelp("→", "next choice")),
		Apply:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
		Cancel:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),

		Interval: key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "interval")),
		About:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "market / about")),
		Browse:   key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "website")),
		Copy:     key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy slug")),
		CopyID:   key.NewBinding(key.WithKeys("Y"), key.WithHelp("Y", "copy condition ID")),

		Help: key.NewBinding(key.WithKeys("h", "?"), key.WithHelp("h", "help")),

		Quit:      key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		Interrupt: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	}
}
