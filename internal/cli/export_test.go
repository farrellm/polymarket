package cli

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/farrellm/polymarket/internal/api"
)

func TestListFlagsToFilter(t *testing.T) {
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		args []string
		want func(f *filter)
	}{
		{
			name: "no flags: open, by 24 hour volume, largest first",
			want: func(*filter) {},
		},
		{
			name: "tag",
			args: []string{"--tag", "politics"},
			want: func(f *filter) { f.tagSlug = "politics" },
		},
		{
			name: "closed",
			args: []string{"--closed"},
			want: func(f *filter) { f.status = api.StatusClosed },
		},
		{
			name: "closed=false is the default spelt out",
			args: []string{"--closed=false"},
			want: func(*filter) {},
		},
		{
			name: "all",
			args: []string{"--all"},
			want: func(f *filter) { f.status = api.StatusAll },
		},
		{
			name: "an order is ascending unless told otherwise",
			args: []string{"--order", "endDate"},
			want: func(f *filter) {
				f.order = sortOrders[5]
				f.ascending = true
			},
		},
		{
			name: "order with desc",
			args: []string{"--order", "liquidity", "--desc"},
			want: func(f *filter) { f.order = sortOrders[4] },
		},
		{
			name: "desc alone keeps the default order",
			args: []string{"--desc"},
			want: func(*filter) {},
		},
		{
			name: "floors",
			args: []string{"--min-volume", "10000", "--min-liquidity", "2.5"},
			want: func(f *filter) {
				f.volumeMin = 10000
				f.liquidityMin = 2.5
			},
		},
		{
			name: "end dates as a day and as a timestamp",
			args: []string{"--ends-after", "2026-11-03", "--ends-before", "2026-11-04T01:30:00+01:00"},
			want: func(f *filter) {
				f.endDateMin = day
				f.endDateMax = day.Add(24*time.Hour + 30*time.Minute)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var lf listFlags
			cmd := &cobra.Command{
				Use:  "markets",
				RunE: func(*cobra.Command, []string) error { return nil },
			}
			bindListFlags(cmd, &lf)
			cmd.SetArgs(tt.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}

			got, err := lf.filter()
			if err != nil {
				t.Fatal(err)
			}
			want := filter{order: sortOrders[0]}
			tt.want(&want)
			if got != want {
				t.Errorf("filter = %+v\nwant     %+v", got, want)
			}
		})
	}
}

func TestListFlagsRejected(t *testing.T) {
	tests := []struct {
		name string
		lf   listFlags
		want string
	}{
		{"an unknown order", listFlags{order: "volumeNum"}, "unknown --order"},
		{"a date that is not one", listFlags{endsAfter: "next week"}, "--ends-after"},
		{"a negative floor", listFlags{minVolume: -1}, "negative"},
		{"a negative limit", listFlags{limit: -1}, "--limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.lf.filter()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
}

// TestOrderFieldsPerEndpoint pins the two names that differ: sorting markets
// by volume has to ask for the numeric field.
func TestOrderFieldsPerEndpoint(t *testing.T) {
	for _, tt := range []struct{ order, events, markets string }{
		{"volume", "volume", "volumeNum"},
		{"liquidity", "liquidity", "liquidityNum"},
		{"volume24hr", "volume24hr", "volume24hr"},
	} {
		f, err := (&listFlags{order: tt.order}).filter()
		if err != nil {
			t.Fatal(err)
		}
		if got := f.eventsQuery().Order; got != tt.events {
			t.Errorf("--order %s sorts events by %q, want %q", tt.order, got, tt.events)
		}
		if got := f.marketsQuery().Order; got != tt.markets {
			t.Errorf("--order %s sorts markets by %q, want %q", tt.order, got, tt.markets)
		}
	}
}

// complete runs the hidden __complete command the shell scripts call, and
// returns the candidate lines and the directive cobra reported.
func complete(t *testing.T, args ...string) (cands []string, directive string) {
	t.Helper()

	cmd := newCommand(&options{})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(append([]string{cobra.ShellCompRequestCmd}, args...))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("__complete %v: %v", args, err)
	}

	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "Completion ended with directive: "):
			directive = strings.TrimPrefix(line, "Completion ended with directive: ")
		case strings.HasPrefix(line, ":"):
			// The numeric directive, repeated for the shell.
		default:
			// Descriptions are tab separated from the value.
			cands = append(cands, strings.SplitN(line, "\t", 2)[0])
		}
	}
	return cands, directive
}

func TestCompletion(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "export offers the datasets",
			args: []string{"export", ""},
			want: []string{"markets", "events", "outcomes"},
		},
		{
			name: "order offers the sort fields",
			args: []string{"export", "markets", "--order", ""},
			want: []string{"volume24hr", "volume1wk", "volume1mo", "volume", "liquidity", "endDate", "startDate"},
		},
		{
			name: "order narrows on what is typed",
			args: []string{"export", "events", "--order", "vol"},
			want: []string{"volume24hr", "volume1wk", "volume1mo", "volume"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cands, directive := complete(t, tt.args...)
			if directive != "ShellCompDirectiveNoFileComp" {
				t.Errorf("directive = %s, want ShellCompDirectiveNoFileComp", directive)
			}
			for _, want := range tt.want {
				if !slices.Contains(cands, want) {
					t.Errorf("candidates %q missing %q", cands, want)
				}
			}
		})
	}
}

// TestFlagCompletionIsRegistered catches a dataset added without the shared
// flags, whatever shape the completion output takes.
func TestFlagCompletionIsRegistered(t *testing.T) {
	export, _, err := newCommand(&options{}).Find([]string{"export"})
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range export.Commands() {
		if _, ok := cmd.GetFlagCompletionFunc("order"); !ok {
			t.Errorf("export %s: no completion function registered for --order", cmd.Name())
		}
	}
}

// Two markets of one event, the second with three outcomes, and the first
// page of two when the server is asked for pages.
const (
	marketOne = `{"id": "1", "slug": "first", "question": "First?", "conditionId": "0x1",
		"outcomes": "[\"Yes\", \"No\"]", "outcomePrices": "[\"0.6\", \"0.4\"]", "clobTokenIds": "[\"11\", \"12\"]",
		"volume": "1500.5", "events": [{"id": "9", "slug": "ev", "title": "Ev"}],
		"tags": [{"id": "2", "label": "Politics", "slug": "politics"}]}`
	marketTwo = `{"id": "2", "slug": "second", "question": "Second?", "conditionId": "0x2",
		"outcomes": "[\"A\", \"B\", \"C\"]", "outcomePrices": "[\"0.5\", \"0.3\", \"0.2\"]",
		"events": [{"id": "9", "slug": "ev", "title": "Ev"}]}`
	eventOne = `{"id": "9", "slug": "ev", "title": "Ev", "volume": 2000, "markets": [` + marketOne + `, ` + marketTwo + `]}`
)

// service is a stand-in for Gamma that serves the listings in two pages and
// remembers what it was asked.
type service struct {
	*httptest.Server

	mu       sync.Mutex
	requests []*url.URL
}

func newService(t *testing.T) *service {
	t.Helper()
	s := &service{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, r.URL)
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		second := r.URL.Query().Get("after_cursor") != ""
		switch r.URL.Path {
		case "/gamma/tags/slug/politics":
			w.Write([]byte(`{"id": "2", "label": "Politics", "slug": "politics"}`))
		case "/gamma/markets/keyset":
			if second {
				w.Write([]byte(`{"markets": [` + marketTwo + `]}`))
				return
			}
			w.Write([]byte(`{"markets": [` + marketOne + `], "next_cursor": "page2"}`))
		case "/gamma/events/keyset":
			if second {
				w.Write([]byte(`{"events": []}`))
				return
			}
			w.Write([]byte(`{"events": [` + eventOne + `], "next_cursor": "page2"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"type": "not found error", "error": "not found"}`))
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// asked returns the queries of the requests made to a path, in order.
func (s *service) asked(path string) []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	var qs []url.Values
	for _, u := range s.requests {
		if u.Path == path {
			qs = append(qs, u.Query())
		}
	}
	return qs
}

// run executes the command line against the stand-in service.
func (s *service) run(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := newCommand(&options{})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append(args, "--gamma-url", s.URL+"/gamma"))
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func readCSV(t *testing.T, text string) [][]string {
	t.Helper()
	records, err := csv.NewReader(strings.NewReader(text)).ReadAll()
	if err != nil {
		t.Fatalf("not CSV: %v\n%s", err, text)
	}
	return records
}

func wantParams(t *testing.T, q url.Values, want map[string]string) {
	t.Helper()
	for key, value := range want {
		if got := q.Get(key); got != value {
			t.Errorf("query %s = %q, want %q (whole query: %s)", key, got, value, q.Encode())
		}
	}
}

func TestExportMarketsToStdout(t *testing.T) {
	s := newService(t)
	stdout, stderr, err := s.run(t, "export", "markets", "--tag", "politics", "--min-volume", "1000")
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing beside the data when it goes to stdout", stderr)
	}

	records := readCSV(t, stdout)
	if len(records) != 3 {
		t.Fatalf("%d records, want the header and both pages' markets", len(records))
	}
	if records[0][0] != "id" || records[1][0] != "1" || records[2][0] != "2" {
		t.Errorf("first column = %q %q %q, want id 1 2", records[0][0], records[1][0], records[2][0])
	}

	pages := s.asked("/gamma/markets/keyset")
	if len(pages) != 2 {
		t.Fatalf("%d listing requests, want 2", len(pages))
	}
	wantParams(t, pages[0], map[string]string{
		"tag_id":         "2", // looked up from the slug
		"closed":         "false",
		"order":          "volume24hr",
		"ascending":      "false",
		"limit":          "100",
		"include_tag":    "true",
		"volume_num_min": "1000",
		"after_cursor":   "",
	})
	wantParams(t, pages[1], map[string]string{"after_cursor": "page2", "tag_id": "2"})
}

func TestExportEvents(t *testing.T) {
	s := newService(t)
	stdout, _, err := s.run(t, "export", "events", "--all", "--order", "volume", "--desc", "--ends-before", "2026-11-03")
	if err != nil {
		t.Fatal(err)
	}
	records := readCSV(t, stdout)
	if len(records) != 2 || records[1][1] != "ev" {
		t.Fatalf("records = %q, want the header and the one event", records)
	}

	pages := s.asked("/gamma/events/keyset")
	if len(pages) != 2 {
		t.Fatalf("%d listing requests, want 2", len(pages))
	}
	wantParams(t, pages[0], map[string]string{
		"order":        "volume",
		"ascending":    "false",
		"end_date_max": "2026-11-03T00:00:00Z",
	})
	if pages[0].Has("closed") {
		t.Errorf("--all sent closed=%s, want it left out", pages[0].Get("closed"))
	}
	if len(s.asked("/gamma/tags/slug/politics")) != 0 {
		t.Error("a tag was looked up with no --tag given")
	}
}

func TestExportOutcomesWithLimit(t *testing.T) {
	s := newService(t)
	stdout, _, err := s.run(t, "export", "outcomes", "--limit", "3")
	if err != nil {
		t.Fatal(err)
	}
	if records := readCSV(t, stdout); len(records) != 4 {
		t.Errorf("%d records, want the header and 3 rows", len(records))
	}
	// One more than the limit is all a page needs to hold.
	wantParams(t, s.asked("/gamma/markets/keyset")[0], map[string]string{"limit": "4"})
}

func TestExportToFile(t *testing.T) {
	s := newService(t)
	path := filepath.Join(t.TempDir(), "markets.csv")
	stdout, stderr, err := s.run(t, "export", "markets", "-o", path)
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing when writing a file", stdout)
	}
	for _, want := range []string{
		"wrote 2 rows of markets to " + path,
		"note: 1 market has more than two outcomes",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to say %q", stderr, want)
		}
	}
	if strings.Contains(stderr, "\r") {
		t.Errorf("stderr = %q, want no progress when it is not a terminal", stderr)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if records := readCSV(t, string(b)); len(records) != 3 {
		t.Errorf("%d records in the file, want 3", len(records))
	}
}

func TestExportToFileReportsTheCap(t *testing.T) {
	s := newService(t)
	path := filepath.Join(t.TempDir(), "markets.csv")
	_, stderr, err := s.run(t, "export", "markets", "-o", path, "--limit", "1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"wrote 1 row of markets", "stopped at the --limit of 1"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to say %q", stderr, want)
		}
	}
}

func TestExportUnknownTag(t *testing.T) {
	s := newService(t)
	for _, dataset := range []string{"markets", "events", "outcomes"} {
		path := filepath.Join(t.TempDir(), "out.csv")
		_, _, err := s.run(t, "export", dataset, "--tag", "nonesuch", "-o", path)
		if err == nil || !strings.Contains(err.Error(), `no tag with the slug "nonesuch"`) {
			t.Errorf("export %s: err = %v, want it to name the missing tag", dataset, err)
		}
		if _, statErr := os.Stat(path); statErr == nil {
			t.Errorf("export %s: a file was written for a tag that does not exist", dataset)
		}
	}
}

func TestExportRejectsBadUsage(t *testing.T) {
	s := newService(t)
	for _, args := range [][]string{
		{"export", "candles"},
		{"export", "markets", "stray"},
		{"export", "markets", "--closed", "--all"},
		{"export", "markets", "--order", "bogus"},
	} {
		if _, _, err := s.run(t, args...); err == nil {
			t.Errorf("%v was accepted, want an error", args)
		}
	}
	if n := len(s.asked("/gamma/markets/keyset")); n != 0 {
		t.Errorf("%d requests made for commands that should not have run", n)
	}
}

func TestExportAloneListsTheDatasets(t *testing.T) {
	s := newService(t)
	stdout, _, err := s.run(t, "export")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range listDatasets {
		if !strings.Contains(stdout, d.name) {
			t.Errorf("help %q does not mention %s", stdout, d.name)
		}
	}
}
