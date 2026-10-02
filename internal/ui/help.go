package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
)

// helpGroup is one heading of the help and the keys under it.
type helpGroup struct {
	title    string
	bindings []key.Binding
}

// helpColumns are the help's two columns of groups. They are built from the
// bindings themselves, so the help cannot disagree with what a key does.
func (k keyMap) helpColumns() [2][]helpGroup {
	return [2][]helpGroup{
		{
			{"Moving", []key.Binding{k.Up, k.Down, k.PageUp, k.PageDown, k.HalfUp, k.HalfDown, k.Top, k.Bottom}},
			{"Levels", []key.Binding{k.Open, k.Back, k.Tags, k.Tab}},
		},
		{
			{"Lists", []key.Binding{k.Find, k.Search, k.Filter, k.SubTag, k.Sort, k.Reverse, k.Refresh}},
			{"Filter form", []key.Binding{k.Next, k.Previous, k.Right, k.Apply, k.Cancel}},
			{"", []key.Binding{k.Help, k.Quit, k.Interrupt}},
		},
	}
}

// closeHelp is what the status bar advertises while the help is up.
func (k keyMap) closeHelp() key.Binding {
	return key.NewBinding(key.WithKeys("esc"), key.WithHelp("any key", "back"))
}

// helpView lists every key, in two columns.
func helpView(k keyMap, st styles, w int) string {
	const keyWidth = 10
	half := max(w/2, 1)

	var columns [2][]string
	for i, groups := range k.helpColumns() {
		for _, g := range groups {
			if len(columns[i]) > 0 {
				columns[i] = append(columns[i], "")
			}
			if g.title != "" {
				columns[i] = append(columns[i], " "+st.header.Render(g.title))
			}
			for _, b := range g.bindings {
				h := b.Help()
				columns[i] = append(columns[i],
					" "+st.hintKey.Render(fit(h.Key, keyWidth, false))+" "+st.hint.Render(h.Desc))
			}
		}
	}

	lines := make([]string, max(len(columns[0]), len(columns[1])))
	for i := range lines {
		var left, right string
		if i < len(columns[0]) {
			left = columns[0][i]
		}
		if i < len(columns[1]) {
			right = columns[1][i]
		}
		lines[i] = fit(left, half, false) + right
	}
	return strings.Join(lines, "\n")
}
