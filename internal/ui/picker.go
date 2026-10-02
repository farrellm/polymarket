package ui

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/farrellm/polymarket/internal/api"
)

// picker chooses one of a tag's sub-tags: a list that narrows as its name is
// typed, as the Tags level does under its find.
type picker struct {
	keys  keyMap
	st    styles
	list  list
	input textinput.Model
	// all is every sub-tag, in the service's ranking; shown is the ones the
	// text typed leaves.
	all, shown []api.Tag
}

func newPicker(e env, subTags []api.Tag) *picker {
	p := &picker{keys: e.keys, st: e.st, input: newPrompt("sub-tag: "), all: subTags}
	p.list = newList(e.st, []column{
		{title: "Sub-tag"},
		{title: "Open events", width: 11, right: true},
	})
	_ = p.input.Focus()
	p.narrow()
	return p
}

// narrow rebuilds the rows from the text typed.
func (p *picker) narrow() {
	needle := strings.ToLower(strings.TrimSpace(p.input.Value()))
	p.shown = p.shown[:0]
	var rows [][]string
	for _, t := range p.all {
		if needle == "" || strings.Contains(strings.ToLower(t.Label), needle) ||
			strings.Contains(strings.ToLower(t.Slug), needle) {
			p.shown = append(p.shown, t)
			rows = append(rows, []string{t.Label, strconv.Itoa(t.ActiveEvents)})
		}
	}
	p.list.empty = "no sub-tag matches " + strconv.Quote(needle)
	p.list.setRows(rows)
	p.list.setCursor(0)
}

func (p *picker) status() string {
	return p.input.View() + "  " + p.st.faint.Render(
		strconv.Itoa(len(p.shown))+" of "+strconv.Itoa(len(p.all))+" sub-tags")
}

// update handles a key. done reports that the picker is finished with, and
// chosen is the sub-tag picked, nil if it was dismissed.
func (p *picker) update(msg tea.KeyPressMsg) (cmd tea.Cmd, chosen *api.Tag, done bool) {
	k := p.keys
	switch {
	case key.Matches(msg, k.Open):
		if p.list.cursor < len(p.shown) {
			return nil, &p.shown[p.list.cursor], true
		}
	case key.Matches(msg, k.Cancel):
		return nil, nil, true
	case key.Matches(msg, k.Up):
		p.list.moveBy(-1)
	case key.Matches(msg, k.Down):
		p.list.moveBy(+1)
	case msg.String() == "pgup":
		p.list.moveBy(-p.list.pageSize())
	case msg.String() == "pgdown":
		p.list.moveBy(+p.list.pageSize())
	default:
		return p.edit(msg), nil, false
	}
	return nil, nil, false
}

// edit hands the prompt a message that is its to read.
func (p *picker) edit(msg tea.Msg) tea.Cmd {
	before := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != before {
		p.narrow()
	}
	return cmd
}
