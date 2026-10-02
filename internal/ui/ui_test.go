package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/farrellm/polymarket/internal/api"
)

// fakePage is what the stand-in listing answers one cursor with.
type fakePage struct {
	events []api.Event
	next   string
}

// fakeMarkets is what the stand-in markets listing answers one cursor with.
type fakeMarkets struct {
	markets []api.Market
	next    string
}

// fakeClient stands in for the service. Its answers are immediate, so a test
// can run every command a keystroke sets off and look at the result.
type fakeClient struct {
	// pages are keyed by the cursor that asks for them; the first is "". A
	// page is narrowed to the tag and the title the query asks for, as the
	// service would.
	pages map[string]fakePage
	// marketPages are keyed the same way, and handed out as they are.
	marketPages map[string]fakeMarkets
	// tags are keyed by slug. Any other slug is not found.
	tags map[string]api.Tag
	// related are a tag's sub-tags, keyed by its slug.
	related map[string][]api.Tag
	// found are the pages of a search, the first being 1.
	found map[int]api.SearchResult
	// event is what a refresh of an event answers, keyed by ID.
	event map[string]api.Event
	// fail, when set, is what the listings answer with instead.
	fail error

	queries       []api.EventsQuery
	marketQueries []api.MarketsQuery
	searches      []api.SearchQuery
	slugs         []string
	relatedSlugs  []string
	eventIDs      []string
	// done holds the Done channel of each request's context.
	done []<-chan struct{}
}

func (c *fakeClient) Events(ctx context.Context, q api.EventsQuery) ([]api.Event, string, error) {
	c.queries = append(c.queries, q)
	c.done = append(c.done, ctx.Done())
	if c.fail != nil {
		return nil, "", c.fail
	}
	p := c.pages[q.Cursor]
	var events []api.Event
	for _, e := range p.events {
		under := q.TagID == "" || slices.ContainsFunc(e.Tags, func(t api.Tag) bool { return t.ID == q.TagID })
		if under && strings.Contains(strings.ToLower(e.Title), strings.ToLower(q.TitleSearch)) {
			events = append(events, e)
		}
	}
	return events, p.next, nil
}

func (c *fakeClient) Markets(ctx context.Context, q api.MarketsQuery) ([]api.Market, string, error) {
	c.marketQueries = append(c.marketQueries, q)
	c.done = append(c.done, ctx.Done())
	if c.fail != nil {
		return nil, "", c.fail
	}
	p := c.marketPages[q.Cursor]
	return p.markets, p.next, nil
}

func (c *fakeClient) Event(ctx context.Context, id string) (*api.Event, error) {
	c.eventIDs = append(c.eventIDs, id)
	c.done = append(c.done, ctx.Done())
	if c.fail != nil {
		return nil, c.fail
	}
	e, ok := c.event[id]
	if !ok {
		return nil, &api.Error{Status: http.StatusNotFound, Body: `{"error":"id not found"}`}
	}
	return &e, nil
}

func (c *fakeClient) Tag(ctx context.Context, slug string) (*api.Tag, error) {
	c.slugs = append(c.slugs, slug)
	c.done = append(c.done, ctx.Done())
	tag, ok := c.tags[slug]
	if !ok {
		return nil, &api.Error{Status: http.StatusNotFound, Body: `{"error":"slug not found"}`}
	}
	return &tag, nil
}

func (c *fakeClient) RelatedTags(ctx context.Context, slug string) ([]api.Tag, error) {
	c.relatedSlugs = append(c.relatedSlugs, slug)
	c.done = append(c.done, ctx.Done())
	if c.fail != nil {
		return nil, c.fail
	}
	return c.related[slug], nil
}

func (c *fakeClient) Search(ctx context.Context, q api.SearchQuery) (*api.SearchResult, error) {
	c.searches = append(c.searches, q)
	c.done = append(c.done, ctx.Done())
	if c.fail != nil {
		return nil, c.fail
	}
	res := c.found[max(q.Page, 1)]
	return &res, nil
}

// testNow is the moment the tests' end dates are measured against.
var testNow = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

// newModel starts a browser over the client with the clock stopped.
func newModel(c Client) *Model {
	return New(c, Options{Now: func() time.Time { return testNow }})
}

func tag(label string) api.Tag {
	slug := strings.ReplaceAll(strings.ToLower(label), " ", "-")
	return api.Tag{ID: "id-" + slug, Label: label, Slug: slug}
}

func amount(v float64) api.Float { return api.Float{Value: v, Valid: true} }

func event(title string, volume24h, liquidity float64, labels ...string) api.Event {
	e := api.Event{
		ID:        "e-" + title,
		Title:     title,
		Volume24h: amount(volume24h),
		Liquidity: amount(liquidity),
		Markets:   make([]api.Market, 2),
	}
	for _, l := range labels {
		e.Tags = append(e.Tags, tag(l))
	}
	return e
}

// sampleEvents add up to: Sports 2 events $330, Politics 2 events $150,
// Elections 1 event $100, Economy 1 event $50, Soccer 1 event $30.
func sampleEvents() []api.Event {
	return []api.Event{
		event("Election", 100, 1000, "Politics", "Elections"),
		event("Fed decision", 50, 400, "Economy", "Politics"),
		event("Final", 300, 200, "Sports"),
		event("Derby", 30, 100, "Sports", "Soccer"),
	}
}

func onePage(events ...api.Event) *fakeClient {
	return &fakeClient{pages: map[string]fakePage{"": {events: events}}}
}

// manyPages serves pages of a hundred events, each event under its own tag
// and under Everything, with a page left over beyond the sample.
func manyPages(pages int) *fakeClient {
	c := &fakeClient{pages: map[string]fakePage{}}
	for p := range pages {
		var page fakePage
		for i := range samplePage {
			n := strconv.Itoa(p*samplePage + i)
			page.events = append(page.events, event("event "+n, 1, 1, "Tag "+n, "Everything"))
		}
		if p < pages-1 {
			page.next = "cursor" + strconv.Itoa(p+1)
		}
		cursor := ""
		if p > 0 {
			cursor = "cursor" + strconv.Itoa(p)
		}
		c.pages[cursor] = page
	}
	return c
}

// messages runs a command and returns what it produced, a batch unpacked.
func messages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, messages(c)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

// step runs a command and delivers its messages, returning the commands they
// set off in turn: one round of the program's loop.
func step(m *Model, cmd tea.Cmd) tea.Cmd {
	var next []tea.Cmd
	for _, msg := range messages(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			continue
		}
		_, c := m.Update(msg)
		next = append(next, c)
	}
	return tea.Batch(next...)
}

// settle runs a command and everything that follows from it.
func settle(m *Model, cmd tea.Cmd) {
	for cmd != nil {
		cmd = step(m, cmd)
	}
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

// press sends keys, letting each one's requests finish before the next.
func press(m *Model, keys ...string) {
	for _, k := range keys {
		_, cmd := m.Update(keyMsg(k))
		settle(m, cmd)
	}
}

// typeText presses each character of s.
func typeText(m *Model, s string) {
	for _, r := range s {
		press(m, string(r))
	}
}

// open starts a browser over the client and lets the sample load.
func open(t *testing.T, c Client) *Model {
	t.Helper()
	m := newModel(c)
	settle(m, m.Init())
	return m
}

// lines renders the frame with styling stripped, so assertions read as the
// user sees the screen.
func lines(m *Model) []string {
	return strings.Split(ansi.Strip(m.View().Content), "\n")
}

func screenText(m *Model) string { return strings.Join(lines(m), "\n") }

// cursorLine is the row the cursor is on.
func cursorLine(t *testing.T, m *Model) string {
	t.Helper()
	for _, l := range lines(m) {
		if strings.Contains(l, cursorMarker) && strings.HasPrefix(l, "│") {
			return l
		}
	}
	t.Fatalf("no row has the cursor:\n%s", screenText(m))
	return ""
}

// headerLine is the line the list's header is on: the first under the title
// bar, or the second where a strip of sub-tags comes between.
func headerLine(m *Model) int {
	if strings.Contains(lines(m)[1], "Sub-tags:") {
		return 2
	}
	return 1
}

// rowLabels are the first cell of each row on screen, header aside.
func rowLabels(m *Model) []string {
	all := lines(m)
	var out []string
	for _, l := range all[headerLine(m)+1 : len(all)-3] {
		l = strings.TrimLeft(strings.Trim(l, "│"), cursorMarker+" ")
		if label, _, _ := strings.Cut(l, "  "); label != "" {
			out = append(out, label)
		}
	}
	return out
}

func wantContains(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to contain %q", what, got, want)
		}
	}
}

func wantLabels(t *testing.T, m *Model, want ...string) {
	t.Helper()
	got := rowLabels(m)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func statusLine(m *Model) string {
	all := lines(m)
	return all[len(all)-2]
}

func TestAggregate(t *testing.T) {
	events := sampleEvents()
	// An event with no figures at all, and a tag listed on it twice.
	bare := api.Event{ID: "bare", Tags: []api.Tag{tag("Sports"), tag("Sports"), {Label: "no id"}}}
	events = append(events, bare)

	all, rows := aggregate(events)
	if all.events != 5 || all.volume24h != 480 || all.liquidity != 1700 {
		t.Errorf("all = %+v, want 5 events, 480 volume, 1700 liquidity", all)
	}
	if !all.isAll() || all.label() != "All" {
		t.Errorf("all = %+v, want the All row", all)
	}

	type sums struct {
		events            int
		volume, liquidity float64
	}
	want := []struct {
		label string
		sums
	}{
		// In the order first met.
		{"Politics", sums{2, 150, 1400}},
		{"Elections", sums{1, 100, 1000}},
		{"Economy", sums{1, 50, 400}},
		{"Sports", sums{3, 330, 300}},
		{"Soccer", sums{1, 30, 100}},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for i, w := range want {
		got := rows[i]
		if got.label() != w.label || (sums{got.events, got.volume24h, got.liquidity}) != w.sums {
			t.Errorf("row %d = %+v, want %s %+v", i, got, w.label, w.sums)
		}
	}
}

// The recorded page changes with every recording, so this checks only what
// must hold of any page.
func TestAggregateFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "events.json"))
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Events []api.Event `json:"events"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Events) == 0 {
		t.Fatal("the fixture holds no events")
	}

	all, rows := aggregate(page.Events)
	if all.events != len(page.Events) {
		t.Errorf("all counts %d events, want %d", all.events, len(page.Events))
	}
	if len(rows) == 0 {
		t.Fatal("no tags came out of the fixture")
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if r.tag.ID == "" || r.tag.Label == "" || r.tag.Slug == "" {
			t.Errorf("row %+v lacks an ID, a label or a slug", r)
		}
		if seen[r.tag.ID] {
			t.Errorf("tag %s has two rows", r.tag.ID)
		}
		seen[r.tag.ID] = true
		if r.events < 1 || r.events > all.events {
			t.Errorf("tag %s counts %d events of %d", r.tag.Slug, r.events, all.events)
		}
		if got := len(eventsUnder(page.Events, r.tag.ID)); got != r.events {
			t.Errorf("tag %s: %d events are filed under it, but it counts %d", r.tag.Slug, got, r.events)
		}
		if r.volume24h > all.volume24h || r.liquidity > all.liquidity {
			t.Errorf("tag %s adds up to more than every event together: %+v", r.tag.Slug, r)
		}
	}
	for _, e := range page.Events {
		for _, tg := range e.Tags {
			if !seen[tg.ID] {
				t.Errorf("tag %s of event %s has no row", tg.Slug, e.ID)
			}
		}
	}
}

func TestSortTags(t *testing.T) {
	_, rows := aggregate(sampleEvents())
	cases := []struct {
		order    tagOrder
		reversed bool
		want     string
	}{
		{byVolume, false, "Sports Politics Elections Economy Soccer"},
		{byVolume, true, "Soccer Economy Elections Politics Sports"},
		// Ties go by name, whichever way round the sort is.
		{byEvents, false, "Politics Sports Economy Elections Soccer"},
		{byEvents, true, "Economy Elections Soccer Politics Sports"},
		{byLiquidity, false, "Politics Elections Economy Sports Soccer"},
		{byName, false, "Economy Elections Politics Soccer Sports"},
		{byName, true, "Sports Soccer Politics Elections Economy"},
	}
	for _, c := range cases {
		sortTags(rows, c.order, c.reversed)
		labels := make([]string, len(rows))
		for i, r := range rows {
			labels[i] = r.label()
		}
		if got := strings.Join(labels, " "); got != c.want {
			t.Errorf("order %d reversed %v: %s, want %s", c.order, c.reversed, got, c.want)
		}
	}
}

func TestTagsLevel(t *testing.T) {
	m := open(t, onePage(sampleEvents()...))

	got := lines(m)
	wantContains(t, "title bar", got[0], "polymarket", "Tags", "ranked over the top 4 open events")
	wantContains(t, "header", got[1], "Tag", "Events", "Vol 24h ↓", "Liquidity")
	// All comes first whatever the sort, then the tags by volume.
	wantLabels(t, m, "All", "Sports", "Politics", "Elections", "Economy", "Soccer")
	wantContains(t, "the All row", got[2], cursorMarker+"All", "4", "$480", "$1.7K")
	wantContains(t, "the Sports row", got[3], "Sports", "2", "$330", "$300")
	wantContains(t, "status bar", statusLine(m), "5 tags", "/ find tag", "s sort", "r refresh", "q quit")
}

func TestSampleIsFiveSequentialPages(t *testing.T) {
	c := manyPages(6)
	m := open(t, c)

	if len(c.queries) != sampleSize/samplePage {
		t.Fatalf("made %d requests, want %d", len(c.queries), sampleSize/samplePage)
	}
	for i, q := range c.queries {
		wantCursor := ""
		if i > 0 {
			wantCursor = "cursor" + strconv.Itoa(i)
		}
		if q.Cursor != wantCursor || q.Limit != samplePage || q.Order != "volume24hr" ||
			q.Ascending || q.Status != api.StatusOpen {
			t.Errorf("request %d = %+v, want cursor %q, the busiest open events first", i, q, wantCursor)
		}
	}
	tg := m.stack[0].(*tags)
	if len(tg.events) != sampleSize || tg.loading || !tg.complete {
		t.Errorf("holds %d events, loading %v, complete %v; want the full sample, loaded",
			len(tg.events), tg.loading, tg.complete)
	}
	wantContains(t, "title bar", lines(m)[0], "top 500 open events")
	wantContains(t, "status bar", statusLine(m), "501 tags")
}

func TestSampleEndsWithTheListing(t *testing.T) {
	c := manyPages(2)
	m := open(t, c)
	if len(c.queries) != 2 {
		t.Errorf("made %d requests, want 2", len(c.queries))
	}
	tg := m.stack[0].(*tags)
	if len(tg.events) != 2*samplePage || tg.loading {
		t.Errorf("holds %d events, loading %v; want 200, loaded", len(tg.events), tg.loading)
	}
}

func TestRowsAppearAsPagesArrive(t *testing.T) {
	m := newModel(manyPages(3))
	wantContains(t, "the empty list", screenText(m), "loading the top 500 open events")

	cmd := step(m, m.Init())
	wantContains(t, "title bar", lines(m)[0], "top 100 open events")
	wantContains(t, "status bar", statusLine(m), "101 tags", "loading: 100 of 500 events")
	wantContains(t, "first row", lines(m)[2], cursorMarker+"All", "100")

	cmd = step(m, cmd)
	wantContains(t, "status bar", statusLine(m), "201 tags", "loading: 200 of 500 events")

	settle(m, cmd)
	if got := statusLine(m); strings.Contains(got, "loading") {
		t.Errorf("status bar = %q after the last page, want no loading note", got)
	}
}

func TestRepeatedCursorIsAnError(t *testing.T) {
	c := &fakeClient{pages: map[string]fakePage{
		"":     {events: sampleEvents(), next: "same"},
		"same": {events: sampleEvents(), next: "same"},
	}}
	m := open(t, c)
	if len(c.queries) != 2 {
		t.Errorf("made %d requests, want 2", len(c.queries))
	}
	wantContains(t, "status bar", statusLine(m), "could not load the events")
}

func TestStalePageIsDropped(t *testing.T) {
	m := open(t, onePage(sampleEvents()...))
	tg := m.stack[0].(*tags)

	settle(m, func() tea.Msg {
		return samplePageMsg{gen: tg.gen - 1, events: []api.Event{event("Stale", 1, 1, "Stale")}}
	})
	if got := screenText(m); strings.Contains(got, "Stale") {
		t.Errorf("a page of an abandoned load was shown:\n%s", got)
	}
	settle(m, func() tea.Msg {
		return samplePageMsg{gen: tg.gen - 1, err: errors.New("stale failure")}
	})
	if got := statusLine(m); strings.Contains(got, "stale failure") {
		t.Errorf("status bar = %q, want the failure of an abandoned load dropped", got)
	}
}

func TestRefreshKeepsRowsUntilTheNewSampleIsComplete(t *testing.T) {
	c := onePage(sampleEvents()...)
	m := open(t, c)
	press(m, "down", "down") // Politics

	c.pages = map[string]fakePage{
		"":  {events: []api.Event{event("New one", 900, 1, "Fresh")}, next: "n"},
		"n": {events: []api.Event{event("New two", 10, 1, "Politics")}},
	}
	_, cmd := m.Update(keyMsg("r"))
	wantContains(t, "status bar", statusLine(m), "5 tags", "refreshing…")
	wantLabels(t, m, "All", "Sports", "Politics", "Elections", "Economy", "Soccer")

	// Half of the new sample is not yet a ranking worth showing.
	cmd = step(m, cmd)
	wantLabels(t, m, "All", "Sports", "Politics", "Elections", "Economy", "Soccer")

	settle(m, cmd)
	wantLabels(t, m, "All", "Fresh", "Politics")
	// The cursor followed its tag to where it now ranks.
	wantContains(t, "cursor row", cursorLine(t, m), "Politics")
	if got := statusLine(m); strings.Contains(got, "refreshing") {
		t.Errorf("status bar = %q, want the refresh finished", got)
	}
}

func TestFailedRefreshKeepsTheRows(t *testing.T) {
	c := onePage(sampleEvents()...)
	m := open(t, c)

	c.fail = errors.New("boom")
	press(m, "r")
	wantLabels(t, m, "All", "Sports", "Politics", "Elections", "Economy", "Soccer")
	wantContains(t, "status bar", statusLine(m), "5 tags", "could not load the events: boom", "r retries")

	c.fail = nil
	press(m, "r")
	if got := statusLine(m); strings.Contains(got, "could not load") {
		t.Errorf("status bar = %q, want the failure gone after a retry that worked", got)
	}
}

func TestFailedFirstLoad(t *testing.T) {
	c := onePage(sampleEvents()...)
	c.fail = errors.New("no route to host")
	m := open(t, c)
	wantContains(t, "status bar", statusLine(m), "0 tags", "could not load the events: no route to host")
	wantContains(t, "the empty list", lines(m)[2], "the events could not be loaded")

	c.fail = nil
	press(m, "r")
	wantLabels(t, m, "All", "Sports", "Politics", "Elections", "Economy", "Soccer")
}

func TestFindNarrowsAsYouType(t *testing.T) {
	m := open(t, onePage(sampleEvents()...))

	press(m, "/")
	typeText(m, "S")
	// By label or by slug, whatever the case; All is a row like any other.
	wantLabels(t, m, "Sports", "Politics", "Elections", "Soccer")
	wantContains(t, "status bar", statusLine(m), "/S", "4 of 5 tags", "enter open", "esc clear")

	typeText(m, "o")
	wantLabels(t, m, "Soccer")
	press(m, "backspace")
	wantLabels(t, m, "Sports", "Politics", "Elections", "Soccer")
	// Each change of the text puts the cursor on the best match.
	wantContains(t, "cursor row", cursorLine(t, m), "Sports")

	// The keys that are not text still move through the list.
	press(m, "down")
	wantContains(t, "cursor row", cursorLine(t, m), "Politics")

	// Esc drops the find and keeps the cursor on its tag.
	press(m, "esc")
	wantLabels(t, m, "All", "Sports", "Politics", "Elections", "Economy", "Soccer")
	wantContains(t, "cursor row", cursorLine(t, m), "Politics")
	wantContains(t, "status bar", statusLine(m), "5 tags", "/ find tag")
}

func TestFindOpensTheRowAndStaysSet(t *testing.T) {
	m := open(t, onePage(sampleEvents()...))

	press(m, "/")
	typeText(m, "ec")
	wantLabels(t, m, "Elections", "Economy")
	press(m, "down", "enter")
	wantContains(t, "title bar", lines(m)[0], "Tags ▸ Economy")
	wantLabels(t, m, "Fed decision")

	// Back up, the find still narrows the list, and esc now clears it.
	press(m, "esc")
	wantLabels(t, m, "Elections", "Economy")
	wantContains(t, "cursor row", cursorLine(t, m), "Economy")
	wantContains(t, "status bar", statusLine(m), `2 of 5 tags matching "ec"`, "esc clear")
	press(m, "esc")
	wantLabels(t, m, "All", "Sports", "Politics", "Elections", "Economy", "Soccer")

	// With no find set, esc at the top level leads nowhere.
	press(m, "esc")
	if len(m.stack) != 1 {
		t.Errorf("stack is %d deep after esc on the Tags level, want 1", len(m.stack))
	}
}

func TestLettersAreTextInThePrompt(t *testing.T) {
	m := open(t, onePage(sampleEvents()...))

	press(m, "/")
	_, cmd := m.Update(keyMsg("q"))
	for _, msg := range messages(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			t.Fatal("q quit while the find prompt was open")
		}
	}
	typeText(m, "rsT")
	if got := m.stack[0].(*tags).input.Value(); got != "qrsT" {
		t.Errorf("find text = %q, want qrsT", got)
	}

	// Ctrl+C still quits.
	_, cmd = m.Update(keyMsg("ctrl+c"))
	quit := false
	for _, msg := range messages(cmd) {
		_, ok := msg.(tea.QuitMsg)
		quit = quit || ok
	}
	if !quit {
		t.Error("ctrl+c did not quit while the find prompt was open")
	}
}

func TestPasteIntoThePrompt(t *testing.T) {
	m := open(t, onePage(sampleEvents()...))
	press(m, "/")
	settle(m, func() tea.Msg { return tea.PasteMsg{Content: "socc"} })
	wantLabels(t, m, "Soccer")
}

func TestUnmatchedFindIsLookedUpBySlug(t *testing.T) {
	c := onePage(sampleEvents()...)
	c.tags = map[string]api.Tag{"nba-finals": tag("NBA Finals")}
	m := open(t, c)

	press(m, "/")
	typeText(m, "  NBA Finals ")
	wantContains(t, "the empty list", lines(m)[2], `no tag here matches "nba finals"`)

	press(m, "enter")
	if len(c.slugs) != 1 || c.slugs[0] != "nba-finals" {
		t.Errorf("looked up %q, want nba-finals", c.slugs)
	}
	wantContains(t, "title bar", lines(m)[0], "Tags ▸ NBA Finals")
	// The tag is asked for by the ID the lookup gave it.
	if q := c.queries[len(c.queries)-1]; q.TagID != "id-nba-finals" {
		t.Errorf("listing asked for tag %q, want id-nba-finals", q.TagID)
	}
	wantContains(t, "the empty list", screenText(m), "no events match: open")
	wantContains(t, "status bar", statusLine(m), "0 loaded · end")
}

func TestLookupOfAnUnknownSlug(t *testing.T) {
	c := onePage(sampleEvents()...)
	m := open(t, c)

	press(m, "/")
	typeText(m, "nosuch")
	press(m, "enter")
	if len(m.stack) != 1 {
		t.Fatalf("an unknown tag was opened: stack is %d deep", len(m.stack))
	}
	wantContains(t, "status bar", statusLine(m), `there is no tag with the slug "nosuch"`)

	// The message lasts until the next key.
	press(m, "down")
	if got := statusLine(m); strings.Contains(got, "there is no tag") {
		t.Errorf("status bar = %q, want the message gone after a key", got)
	}

	// An empty find has nothing to look up.
	press(m, "esc", "/", "enter")
	if len(c.slugs) != 1 {
		t.Errorf("looked up %q, want only the one lookup", c.slugs)
	}
}

func TestOvertakenLookupIsDropped(t *testing.T) {
	c := onePage(sampleEvents()...)
	c.tags = map[string]api.Tag{"nba": tag("NBA")}
	m := open(t, c)

	press(m, "/")
	typeText(m, "nba")
	_, lookup := m.Update(keyMsg("enter"))
	wantContains(t, "status bar", statusLine(m), `looking up the tag "nba"…`)

	// The user clears the find and opens a row before the answer arrives.
	press(m, "esc", "down", "enter")
	wantContains(t, "title bar", lines(m)[0], "Tags ▸ Sports")
	settle(m, lookup)
	if len(m.stack) != 2 {
		t.Errorf("stack is %d deep, want the late answer to open nothing", len(m.stack))
	}
	wantContains(t, "title bar", lines(m)[0], "Tags ▸ Sports")
}

func TestDrillDownAndBackRestoresTheCursor(t *testing.T) {
	m := open(t, onePage(sampleEvents()...))

	press(m, "down", "down", "enter") // Politics
	got := lines(m)
	wantContains(t, "title bar", got[0], "Tags ▸ Politics", "[Events] Markets", "vol 24h ↓ · open")
	wantContains(t, "strip", got[1], "Sub-tags: none")
	wantContains(t, "header", got[2], "Event", "Markets", "Vol 24h", "Volume", "Liq", "Ends")
	wantLabels(t, m, "Election", "Fed decision")
	wantContains(t, "first row", got[3], cursorMarker+"Election", "2", "$100", "–", "$1.0K")
	wantContains(t, "status bar", statusLine(m), "2 loaded · end", "/ search", "f filter", "s sort", "h help")

	press(m, "down", "esc")
	wantContains(t, "title bar", lines(m)[0], "Tags")
	wantContains(t, "cursor row", cursorLine(t, m), "Politics")
	if len(m.stack) != 1 {
		t.Errorf("stack is %d deep after esc, want 1", len(m.stack))
	}

	// All opens every event, and has no sub-tags to show.
	press(m, "g", "enter")
	wantContains(t, "title bar", lines(m)[0], "Tags ▸ All")
	wantContains(t, "header", lines(m)[1], "Event")
	wantLabels(t, m, "Election", "Fed decision", "Final", "Derby")
}

func TestTagsKeyJumpsToTheTop(t *testing.T) {
	c := onePage(sampleEvents()...)
	c.related = map[string][]api.Tag{"sports": {tag("Soccer")}}
	m := open(t, c)
	press(m, "down", "enter", "t", "enter")
	wantContains(t, "title bar", lines(m)[0], "Tags ▸ Sports ▸ Soccer")

	press(m, "T")
	if len(m.stack) != 1 {
		t.Fatalf("stack is %d deep after T, want 1", len(m.stack))
	}
	wantContains(t, "cursor row", cursorLine(t, m), "Sports")
}

func TestSortKeepsTheCursorOnItsTag(t *testing.T) {
	m := open(t, onePage(sampleEvents()...))
	press(m, "down", "down", "down") // Elections

	press(m, "s")
	wantContains(t, "header", lines(m)[1], "Events ↓")
	wantLabels(t, m, "All", "Politics", "Sports", "Economy", "Elections", "Soccer")
	wantContains(t, "cursor row", cursorLine(t, m), "Elections")

	press(m, "s")
	wantContains(t, "header", lines(m)[1], "Liquidity ↓")
	press(m, "s")
	wantContains(t, "header", lines(m)[1], "Tag ↑")
	wantLabels(t, m, "All", "Economy", "Elections", "Politics", "Soccer", "Sports")

	press(m, "S")
	wantContains(t, "header", lines(m)[1], "Tag ↓")
	wantLabels(t, m, "All", "Sports", "Soccer", "Politics", "Elections", "Economy")
	wantContains(t, "cursor row", cursorLine(t, m), "Elections")

	// Round to the start again, the right way up.
	press(m, "s")
	wantContains(t, "header", lines(m)[1], "Vol 24h ↓")
}

func TestCursorMovesAndScrolls(t *testing.T) {
	m := open(t, manyPages(1))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10}) // five rows in view

	press(m, "G")
	wantContains(t, "cursor row", cursorLine(t, m), "Tag 99")
	press(m, "g")
	wantContains(t, "cursor row", cursorLine(t, m), "All")
	// The rows are All, Everything, then the tags by name: 0, 1, 10, 11, …
	press(m, "pgdown")
	wantContains(t, "cursor row", cursorLine(t, m), "Tag 11")
	press(m, "d")
	wantContains(t, "cursor row", cursorLine(t, m), "Tag 13")
	press(m, "u", "pgup", "up")
	wantContains(t, "cursor row", cursorLine(t, m), "All")
	press(m, " ")
	wantContains(t, "cursor row", cursorLine(t, m), "Tag 11")

	// A window grown taller shows more rows rather than blank lines.
	press(m, "G")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if got := len(rowLabels(m)); got != 19 {
		t.Errorf("%d rows in view at the end of the list, want 19", got)
	}
}

// Every line must fit the terminal, or the display wraps and the frame
// breaks up.
func TestViewFitsTheWindow(t *testing.T) {
	long := event(strings.Repeat("A very long title indeed. ", 8), 5e6, 2e9,
		strings.Repeat("An unreasonably long tag ", 6), "日本語のタグ")
	sizes := []struct{ w, h int }{
		{200, 60}, {80, 24}, {60, 20}, {40, 12}, {30, 8}, {12, 5}, {3, 3}, {1, 1}, {0, 0},
	}
	for _, size := range sizes {
		c := onePage(append(sampleEvents(), long)...)
		c.related = map[string][]api.Tag{"日本語のタグ": {tag(strings.Repeat("A long sub-tag ", 9)), tag("Short")}}
		c.marketPages = map[string]fakeMarkets{"": {markets: []api.Market{
			market("m1", strings.Repeat("A very long question? ", 8), "", 0.999, 0.25, 9e9),
		}}}
		m := open(t, c)
		check := func(what string) {
			t.Helper()
			got := lines(m)
			if len(got) > max(size.h, 1) {
				t.Errorf("%dx%d %s: %d lines", size.w, size.h, what, len(got))
			}
			for i, l := range got {
				if w := ansi.StringWidth(l); w > size.w {
					t.Errorf("%dx%d %s: line %d is %d columns: %q", size.w, size.h, what, i, w, l)
				}
			}
		}

		m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		check("tags")
		press(m, "/")
		typeText(m, "a tag that does not exist anywhere")
		check("find")
		press(m, "esc", "G", "enter")
		check("events")
		press(m, "tab")
		check("markets")
		press(m, "f", "tab")
		typeText(m, "an amount that is nothing of the kind, and is long")
		press(m, "enter")
		check("filter form")
		press(m, "esc", "/")
		typeText(m, "a search for something with a long name")
		check("search")
		press(m, "enter", "tab", "h")
		check("help")
		press(m, "h", "t")
		check("sub-tags")
		press(m, "esc", "g", "enter")
		check("an event's markets")
	}
}

func TestFrameAtTheMinimumSize(t *testing.T) {
	m := open(t, onePage(sampleEvents()...))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	got := lines(m)
	if len(got) != 24 {
		t.Fatalf("frame is %d lines, want 24", len(got))
	}
	for i, l := range got {
		if w := ansi.StringWidth(l); w != 80 {
			t.Errorf("line %d is %d columns, want 80: %q", i, w, l)
		}
	}
	if !strings.HasPrefix(got[0], "┌ polymarket ─ Tags ─") || !strings.HasSuffix(got[0], " ┐") {
		t.Errorf("title bar = %q", got[0])
	}
	if !strings.HasPrefix(got[21], "├─") || !strings.HasPrefix(got[23], "└─") {
		t.Errorf("frame = %q … %q", got[21], got[23])
	}
	wantContains(t, "header", got[1], "Tag", "Events", "Vol 24h", "Liquidity")
}

func TestNarrowWindowDropsColumnsFromTheRight(t *testing.T) {
	m := open(t, onePage(sampleEvents()...))

	m.Update(tea.WindowSizeMsg{Width: 44, Height: 12})
	header := lines(m)[1]
	wantContains(t, "header", header, "Tag", "Events", "Vol 24h")
	if strings.Contains(header, "Liquidity") {
		t.Errorf("header = %q, want Liquidity dropped at 44 columns", header)
	}

	m.Update(tea.WindowSizeMsg{Width: 24, Height: 12})
	header = lines(m)[1]
	if strings.Contains(header, "Events") || !strings.Contains(header, "Tag") {
		t.Errorf("header = %q, want only the tag at 24 columns", header)
	}
	wantContains(t, "second row", lines(m)[3], "Sports")
}

func TestQuitStopsTheRequests(t *testing.T) {
	c := manyPages(3)
	m := newModel(c)
	cmd := step(m, m.Init()) // one page in, the next asked for

	_, quit := m.Update(keyMsg("q"))
	found := false
	for _, msg := range messages(quit) {
		_, ok := msg.(tea.QuitMsg)
		found = found || ok
	}
	if !found {
		t.Fatal("q did not quit")
	}
	_ = messages(cmd)
	for i, done := range c.done {
		select {
		case <-done:
		default:
			t.Errorf("request %d was not cancelled on quitting", i)
		}
	}
}

func TestRefreshAbandonsTheLoadUnderWay(t *testing.T) {
	c := manyPages(3)
	m := newModel(c)
	stale := m.Init()

	_, fresh := m.Update(keyMsg("r"))
	// The first load's page arrives after the refresh began.
	settle(m, stale)
	if len(c.queries) != 1 {
		t.Errorf("the abandoned load went on to make %d requests", len(c.queries))
	}
	select {
	case <-c.done[0]:
	default:
		t.Error("the abandoned load's request was not cancelled")
	}

	settle(m, fresh)
	if got := len(m.stack[0].(*tags).events); got != 3*samplePage {
		t.Errorf("holds %d events after the refresh, want 300", got)
	}
}
