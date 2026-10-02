package ui

import (
	"encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/farrellm/polymarket/internal/api"
)

// stamp is the moment of testNow as a default file name carries it.
const stamp = "20261002-120000"

// workDir moves the test into a directory of its own, where a file named
// without one lands.
func workDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	return dir
}

// readCSV reads an exported file back: its header, and its rows.
func readCSV(t *testing.T, path string) (header []string, rows [][]string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Fatalf("%s has no header", path)
	}
	return records[0], records[1:]
}

// cells are one column of an exported file.
func cells(t *testing.T, path, name string) []string {
	t.Helper()
	header, rows := readCSV(t, path)
	at := slices.Index(header, name)
	if at < 0 {
		t.Fatalf("%s has no column %s: %q", path, name, header)
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r[at]
	}
	return out
}

func wantCells(t *testing.T, path, name string, want ...string) {
	t.Helper()
	if got := cells(t, path, name); !slices.Equal(got, want) {
		t.Errorf("%s of %s = %q, want %q", name, filepath.Base(path), got, want)
	}
}

// wantFiles checks that a directory holds these files and no others: an
// export that did not finish leaves nothing behind.
func wantFiles(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(entries))
	for i, e := range entries {
		got[i] = e.Name()
	}
	if !slices.Equal(got, want) {
		t.Errorf("files = %q, want %q", got, want)
	}
}

// retype replaces the text of the field in focus.
func retype(m *Model, text string) {
	m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	typeText(m, text)
}

func TestExportOfTheTags(t *testing.T) {
	dir := workDir(t)
	m := open(t, onePage(sampleEvents()...))
	wantContains(t, "status bar", statusLine(m), "e export")

	press(m, "e")
	screen := screenText(m)
	wantContains(t, "dialog", screen, "Export", cursorMarker+"Dataset", "[tags]", "[the 5 tags listed]",
		"At most        10000", "./polymarket-tags-"+stamp+".csv")
	wantContains(t, "status bar", statusLine(m), "export", "enter export", "esc cancel")
	// The frame is the screen's own.
	wantContains(t, "title bar", lines(m)[0], "polymarket ─ Tags")

	press(m, "enter")
	name := "polymarket-tags-" + stamp + ".csv"
	wantContains(t, "status bar", statusLine(m), "wrote 5 rows to ./"+name)
	wantFiles(t, dir, name)
	// The rows are the ones listed, in the order they are listed in, and
	// the All row is not among them.
	header, _ := readCSV(t, name)
	if got := strings.Join(header, ","); got != "id,slug,label,events,volume_24h,liquidity" {
		t.Errorf("header = %s", got)
	}
	wantCells(t, name, "label", "Sports", "Politics", "Elections", "Economy", "Soccer")
	wantCells(t, name, "events", "2", "2", "1", "1", "1")
	wantCells(t, name, "volume_24h", "330", "150", "100", "50", "30")

	// What was written is said until the next key.
	wantContains(t, "header", lines(m)[1], "Tag")
	press(m, "down")
	if got := statusLine(m); strings.Contains(got, "wrote") {
		t.Errorf("status bar = %q, want the message gone after a key", got)
	}
}

func TestExportFollowsTheFindAndTheSort(t *testing.T) {
	workDir(t)
	m := open(t, onePage(sampleEvents()...))
	press(m, "/")
	typeText(m, "s")
	// Enter would open the row; the find stays set after esc of the level
	// below.
	press(m, "enter", "esc", "s", "s", "s") // by name
	press(m, "e")
	wantContains(t, "dialog", screenText(m), "[the 4 tags listed]")
	press(m, "enter")
	wantCells(t, "polymarket-tags-"+stamp+".csv", "label", "Elections", "Politics", "Soccer", "Sports")
}

func TestExportOfEvents(t *testing.T) {
	workDir(t)
	m, c := politics(t)
	asked := len(c.queries)

	press(m, "e")
	wantContains(t, "dialog", screenText(m), "[events]", "[the 3 loaded]", "all that match")
	press(m, "enter")
	name := "polymarket-events-" + stamp + ".csv"
	wantContains(t, "status bar", statusLine(m), "wrote 3 rows to ./"+name)
	wantCells(t, name, "title", "Nominee 2028", "Election", "Fed decision")
	wantCells(t, name, "markets", "4", "2", "2")
	if len(c.queries) != asked {
		t.Errorf("exporting the rows loaded made %d requests", len(c.queries)-asked)
	}

	// All that match is the listing asked again from its start, as it is
	// filtered and sorted on screen.
	press(m, "S", "/")
	typeText(m, "e")
	press(m, "enter", "e", "tab", "right")
	wantContains(t, "dialog", screenText(m), cursorMarker+"Rows", "[all that match]")
	// The file exists: the first enter asks, and the second overwrites.
	press(m, "enter")
	wantContains(t, "dialog", screenText(m), "./"+name+" exists: enter again overwrites it")
	asked = len(c.queries)
	press(m, "enter")
	wantContains(t, "status bar", statusLine(m), "wrote 3 rows")
	if len(c.queries) != asked+1 {
		t.Fatalf("%d requests for all that match, want 1", len(c.queries)-asked)
	}
	q := lastQuery(t, c)
	if q.TagID != "id-politics" || q.TitleSearch != "e" || !q.Ascending || q.Limit != pageSize || q.Cursor != "" {
		t.Errorf("query = %+v, want the tag's events as searched and sorted, from the start", q)
	}
}

func TestExportStopsAtTheCap(t *testing.T) {
	workDir(t)
	c := manyPages(3)
	m := open(t, c)
	press(m, "enter") // All
	asked := len(c.queries)

	press(m, "e", "tab", "right", "tab")
	wantContains(t, "dialog", screenText(m), "[all that match]", cursorMarker+"At most")
	retype(m, "abc")
	press(m, "enter")
	wantContains(t, "dialog", screenText(m), "At most is not a number of rows such as 10000")
	retype(m, "150")
	if got := screenText(m); strings.Contains(got, "is not a number") {
		t.Errorf("the complaint outlived the text it was about:\n%s", got)
	}
	press(m, "enter")
	wantContains(t, "status bar", statusLine(m), "wrote the first 150 rows to ./polymarket-events-"+stamp+".csv")
	if got := len(cells(t, "polymarket-events-"+stamp+".csv", "id")); got != 150 {
		t.Errorf("%d rows written, want 150", got)
	}
	// Two pages hold the 150 and one row more, which is how it is known
	// that there was more: the third is not asked for.
	if len(c.queries) != asked+2 {
		t.Errorf("%d requests, want 2", len(c.queries)-asked)
	}

	// A blank cap is none.
	press(m, "e", "tab", "right", "tab")
	retype(m, "")
	press(m, "tab")
	retype(m, "all.csv")
	press(m, "enter")
	wantContains(t, "status bar", statusLine(m), "wrote 300 rows to all.csv")
}

func TestExportOfMarkets(t *testing.T) {
	workDir(t)
	m, c := politics(t)
	press(m, "tab")
	if q := c.marketQueries[0]; !q.IncludeTags {
		t.Errorf("query = %+v, want the markets asked for with their tags, which an export writes", q)
	}

	// The markets are to be had a market to a row, or an outcome to one;
	// a file name left alone follows the dataset.
	press(m, "e")
	wantContains(t, "dialog", screenText(m), "[markets]", "outcomes", "[the 2 loaded]", "all that match",
		"polymarket-markets-"+stamp+".csv")
	press(m, "right")
	wantContains(t, "dialog", screenText(m), "[outcomes]", "polymarket-outcomes-"+stamp+".csv")
	press(m, "enter")
	name := "polymarket-outcomes-" + stamp + ".csv"
	wantContains(t, "status bar", statusLine(m), "wrote 4 rows")
	wantCells(t, name, "market_id", "m2", "m2", "m1", "m1")
	wantCells(t, name, "outcome", "Yes", "No", "Yes", "No")

	// A name that was typed stays when the dataset changes.
	press(m, "e", "tab", "tab", "tab")
	retype(m, "mine.csv")
	press(m, "tab", "left", "tab", "right")
	wantContains(t, "dialog", screenText(m), "[outcomes]", "[all that match]", "mine.csv")
	asked := len(c.marketQueries)
	press(m, "enter")
	wantContains(t, "status bar", statusLine(m), "wrote 4 rows to mine.csv")
	if len(c.marketQueries) != asked+1 {
		t.Errorf("%d requests for all that match, want 1", len(c.marketQueries)-asked)
	}
}

func TestExportOfAnEventsMarkets(t *testing.T) {
	workDir(t)
	m, _ := politics(t)
	press(m, "enter", "e")
	// The closed one is filtered out, on screen and so in the file; there
	// is nothing more to fetch.
	screen := screenText(m)
	wantContains(t, "dialog", screen, "[markets]", "outcomes", "[the 3 listed]")
	if strings.Contains(screen, "all that match") {
		t.Errorf("an event's markets are all in hand:\n%s", screen)
	}
	press(m, "enter")
	name := "polymarket-markets-" + stamp + ".csv"
	wantCells(t, name, "id", "m2", "m1", "m4")
	// A market inside its event is written with it, and under its tags.
	wantCells(t, name, "event_slug", "nominee-2028", "nominee-2028", "nominee-2028")
	wantCells(t, name, "tags", "politics", "politics", "politics")
	wantContains(t, "url", cells(t, name, "url")[0], "https://polymarket.com/event/nominee-2028/slug-m2")
}

func TestExportOfAMarket(t *testing.T) {
	workDir(t)
	m, c := bob(t)

	press(m, "e")
	wantContains(t, "dialog", screenText(m), "[history]", "trades", "book",
		"[Yes · 1w, as charted]", "every outcome · 1w")
	histories := len(c.histories)
	press(m, "enter")
	name := "polymarket-history-" + stamp + ".csv"
	wantContains(t, "status bar", statusLine(m), "wrote 4 rows")
	wantCells(t, name, "price", "0.25", "0.24", "0.28", "0.3")
	wantCells(t, name, "outcome", "Yes", "Yes", "Yes", "Yes")
	if len(c.histories) != histories {
		t.Errorf("exporting the chart made %d requests", len(c.histories)-histories)
	}

	// Every outcome is fetched: No has no prices over the week.
	press(m, "e", "tab", "right", "tab", "tab")
	retype(m, "both.csv")
	press(m, "enter")
	wantContains(t, "status bar", statusLine(m), "wrote 4 rows to both.csv")
	got := c.histories[histories:]
	want := []api.HistoryQuery{
		{TokenID: "m2-yes", Interval: "1w", Limit: historyPageSize},
		{TokenID: "m2-no", Interval: "1w", Limit: historyPageSize},
	}
	if !slices.Equal(got, want) {
		t.Errorf("histories asked for: %+v, want %+v", got, want)
	}

	press(m, "e", "right")
	wantContains(t, "dialog", screenText(m), "[trades]", "[the 3 latest]", "all of them")
	press(m, "tab", "right", "enter")
	wantContains(t, "status bar", statusLine(m), "wrote 3 rows")
	if q := c.tradeQueries[len(c.tradeQueries)-1]; q != (api.TradesQuery{ConditionID: "0xm2", Limit: tradesPageSize}) {
		t.Errorf("trades asked for: %+v, want all of the market's", q)
	}

	press(m, "e", "left")
	wantContains(t, "dialog", screenText(m), "[book]", "[Yes, as shown]", "every outcome")
	press(m, "enter")
	name = "polymarket-book-" + stamp + ".csv"
	wantCells(t, name, "side", "bid", "bid", "ask")
	wantCells(t, name, "price", "0.29", "0.28", "0.31")

	press(m, "e", "left", "tab", "right", "enter", "enter") // it exists
	wantContains(t, "status bar", statusLine(m), "wrote 6 rows")
	wantCells(t, name, "outcome", "Yes", "Yes", "Yes", "No", "No", "No")
}

func TestExportThatFails(t *testing.T) {
	dir := workDir(t)
	m, c := bob(t)
	c.fail = errors.New("boom")
	press(m, "e", "tab", "right", "enter")
	wantContains(t, "status bar", statusLine(m), "could not export the history: boom")
	wantFiles(t, dir)
	// The market is still there.
	wantContains(t, "question", lines(m)[1], "Will Bob win?")
}

func TestExportDialogChecksTheFile(t *testing.T) {
	dir := workDir(t)
	t.Setenv("HOME", dir)
	if err := os.Mkdir("sub", 0o755); err != nil {
		t.Fatal(err)
	}
	m := open(t, onePage(sampleEvents()...))
	press(m, "e", "shift+tab")
	wantContains(t, "dialog", screenText(m), cursorMarker+"File")

	for name, want := range map[string]string{
		"":              "File needs the name of a file to write",
		"sub":           "sub is a directory",
		"nowhere/x.csv": "there is no directory nowhere",
	} {
		retype(m, name)
		press(m, "enter")
		wantContains(t, "dialog", screenText(m), want)
	}
	wantFiles(t, dir, "sub")

	// Letters are text here, q among them, and ~ is the home directory.
	retype(m, "~/sub/quiet e.csv")
	press(m, "enter")
	wantContains(t, "status bar", statusLine(m), "wrote 5 rows to ~/sub/quiet e.csv")
	wantFiles(t, filepath.Join(dir, "sub"), "quiet e.csv")

	// Esc leaves without writing.
	press(m, "e", "shift+tab")
	retype(m, "other.csv")
	press(m, "esc")
	wantContains(t, "header", lines(m)[1], "Tag")
	wantFiles(t, dir, "sub")
}

func TestPasteIntoTheExportDialog(t *testing.T) {
	dir := workDir(t)
	m := open(t, onePage(sampleEvents()...))
	press(m, "e", "shift+tab")
	retype(m, "")
	settle(m, func() tea.Msg { return tea.PasteMsg{Content: "pasted.csv"} })
	press(m, "enter")
	wantFiles(t, dir, "pasted.csv")
}

func TestExportRunsWhileTheBrowserIsUsed(t *testing.T) {
	dir := workDir(t)
	m, _ := politics(t)
	press(m, "e")
	_, running := m.Update(keyMsg("enter"))
	name := "./polymarket-events-" + stamp + ".csv"
	wantContains(t, "status bar", statusLine(m), "exporting events: 0 rows", "esc stop export")
	wantContains(t, "header", lines(m)[2], "Event")

	// The count is the export's own news of itself.
	m.Update(exportProgressMsg{job: m.job, rows: 1200})
	wantContains(t, "status bar", statusLine(m), "exporting events: 1200 rows", "esc stop export")

	// The keys are the screen's meanwhile, and a second export waits.
	press(m, "down")
	wantContains(t, "cursor", cursorLine(t, m), "Election")
	press(m, "e")
	wantContains(t, "status bar", statusLine(m), "an export is under way")
	wantContains(t, "header", lines(m)[2], "Event")
	// A prompt has the status bar while it is open.
	press(m, "/")
	wantContains(t, "status bar", statusLine(m), "/")
	press(m, "esc")
	if m.job == nil || m.job.stopping {
		t.Fatal("esc in a prompt stopped the export")
	}

	settle(m, running)
	wantContains(t, "status bar", statusLine(m), "wrote 3 rows to "+name)
	wantFiles(t, dir, filepath.Base(name))
}

func TestEscStopsAnExport(t *testing.T) {
	dir := workDir(t)
	m, _ := politics(t)
	press(m, "e", "tab", "right")
	_, running := m.Update(keyMsg("enter"))

	// Esc is the export's first: the level stays.
	press(m, "esc")
	if len(m.stack) != 2 {
		t.Fatalf("stack is %d deep after esc, want 2", len(m.stack))
	}
	wantContains(t, "status bar", statusLine(m), "stopping the export…")

	settle(m, running)
	wantContains(t, "status bar", statusLine(m), "export cancelled", "was not written")
	wantFiles(t, dir)

	// With none running, esc is the screen's again.
	press(m, "esc")
	if len(m.stack) != 1 {
		t.Errorf("stack is %d deep after a second esc, want 1", len(m.stack))
	}
}

func TestQuitStopsAnExport(t *testing.T) {
	dir := workDir(t)
	m, _ := politics(t)
	press(m, "e", "tab", "right")
	_, running := m.Update(keyMsg("enter"))

	_, quit := m.Update(keyMsg("q"))
	found := false
	for _, msg := range messages(quit) {
		_, ok := msg.(tea.QuitMsg)
		found = found || ok
	}
	if !found {
		t.Fatal("q did not quit")
	}
	// By the time the program quits the export has ended, and taken its
	// temporary file with it.
	wantFiles(t, dir)
	_ = messages(running)
}

func TestNothingToExport(t *testing.T) {
	m := newModel(&fakeClient{})
	press(m, "e")
	wantContains(t, "status bar", statusLine(m), "nothing here to export yet")
	if m.dialog != nil {
		t.Error("the dialog opened on nothing")
	}

	// In a prompt, e is a letter.
	m = open(t, onePage(sampleEvents()...))
	press(m, "/", "e")
	wantContains(t, "status bar", statusLine(m), "/e")
	if m.dialog != nil {
		t.Error("e in a prompt opened the dialog")
	}
}
