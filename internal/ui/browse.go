package ui

import (
	"strconv"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/farrellm/polymarket/internal/api"
	"github.com/farrellm/polymarket/internal/format"
)

// browse is the level under Tags: the events filed under one tag.
//
// For now it lists only the events the Tags level already holds, which are
// the tag's share of the sample the tags were ranked over. The complete
// listing, fetched from the service and paged, sorted and filtered there,
// takes their place later; these rows are what it will show while it loads.
type browse struct {
	keys keyMap
	st   styles

	// tag is the zero Tag for the All row: every event.
	tag    api.Tag
	events []api.Event
	// sample is how many events the ones here were picked from.
	sample int

	list list
}

func newBrowse(keys keyMap, st styles, tag api.Tag, events []api.Event, sample int) *browse {
	b := &browse{keys: keys, st: st, tag: tag, events: events, sample: sample}
	b.list = newList(st, []column{
		{title: "Event"},
		{title: "Markets", width: 7, right: true},
		{title: "Vol 24h", width: 8, right: true},
		{title: "Volume", width: 8, right: true},
		{title: "Liquidity", width: 9, right: true},
	})
	b.list.empty = "none of the top " + strconv.Itoa(sample) + " open events is filed under " + b.crumb()

	rows := make([][]string, len(events))
	for i := range events {
		e := &events[i]
		rows[i] = []string{
			e.Title,
			strconv.Itoa(len(e.Markets)),
			money(e.Volume24h),
			money(e.Volume),
			money(e.Liquidity),
		}
	}
	b.list.setRows(rows)
	return b
}

// money renders an amount the service may not have sent. A missing one is
// not a zero, and is not shown as one.
func money(f api.Float) string {
	if !f.Valid {
		return "–"
	}
	return format.Money(f.Value)
}

func (b *browse) init() tea.Cmd { return nil }

func (b *browse) close() {}

func (b *browse) resize(width, height int) { b.list.setSize(width, height) }

func (b *browse) view() string { return b.list.view() }

func (b *browse) crumb() string {
	if b.tag.ID == "" {
		return "All"
	}
	return b.tag.Label
}

func (b *browse) note() string { return "Events" }

func (b *browse) typing() bool { return false }

func (b *browse) hints() []key.Binding {
	return []key.Binding{b.keys.Back, b.keys.Tags, b.keys.Quit}
}

func (b *browse) status() status {
	return status{text: strconv.Itoa(len(b.events)) + " of the top " + strconv.Itoa(b.sample) + " open events"}
}

func (b *browse) update(msg tea.Msg) (tea.Cmd, nav) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case b.list.handle(msg, b.keys):
		case key.Matches(msg, b.keys.Back):
			return nil, nav{pop: true}
		}
	}
	return nil, nav{}
}
