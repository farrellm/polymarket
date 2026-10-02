package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/farrellm/polymarket/internal/api"
	"github.com/farrellm/polymarket/internal/export"
)

// pageSize is the most the listing endpoints hand out per request.
const pageSize = 100

// sortOrder is a field a listing can be sorted by. The two listings do not
// always call it the same thing.
type sortOrder struct {
	name, desc string
	// events and markets are the field's names on the two endpoints.
	events, markets string
}

// sortOrders are the values --order takes, all checked against the live
// service. On /markets/keyset, order=volume compares the volume as text, so
// the numeric fields are asked for instead.
var sortOrders = []sortOrder{
	{"volume24hr", "volume over the last 24 hours", "volume24hr", "volume24hr"},
	{"volume1wk", "volume over the last week", "volume1wk", "volume1wk"},
	{"volume1mo", "volume over the last month", "volume1mo", "volume1mo"},
	{"volume", "volume since the start", "volume", "volumeNum"},
	{"liquidity", "liquidity", "liquidity", "liquidityNum"},
	{"endDate", "end date", "endDate", "endDate"},
	{"startDate", "start date", "startDate", "startDate"},
}

// defaultOrder is what a listing is sorted by when --order is not given,
// largest first. The service's own default is by ID, oldest first, which
// makes a poor sample of anything cut short by --limit.
const defaultOrder = "volume24hr"

func findOrder(name string) (sortOrder, error) {
	names := make([]string, len(sortOrders))
	for i, o := range sortOrders {
		if o.name == name {
			return o, nil
		}
		names[i] = o.name
	}
	return sortOrder{}, fmt.Errorf("unknown --order %q: it is one of %s", name, strings.Join(names, ", "))
}

// listFlags are the flags of the list datasets, as typed.
type listFlags struct {
	tag          string
	closed       bool
	all          bool
	order        string
	desc         bool
	minVolume    float64
	minLiquidity float64
	endsAfter    string
	endsBefore   string
	limit        int
	output       string
	raw          bool
}

// filter is what a listing is narrowed and sorted by: the flags, checked.
type filter struct {
	tagSlug      string
	status       api.Status
	order        sortOrder
	ascending    bool
	volumeMin    float64
	liquidityMin float64
	endDateMin   time.Time
	endDateMax   time.Time
}

func (lf *listFlags) filter() (filter, error) {
	f := filter{
		tagSlug:      lf.tag,
		volumeMin:    lf.minVolume,
		liquidityMin: lf.minLiquidity,
	}
	switch {
	case lf.all:
		f.status = api.StatusAll
	case lf.closed:
		f.status = api.StatusClosed
	default:
		f.status = api.StatusOpen
	}

	name := lf.order
	if name == "" {
		name = defaultOrder
	} else {
		f.ascending = !lf.desc
	}
	var err error
	if f.order, err = findOrder(name); err != nil {
		return filter{}, err
	}

	if f.endDateMin, err = parseDate("--ends-after", lf.endsAfter); err != nil {
		return filter{}, err
	}
	if f.endDateMax, err = parseDate("--ends-before", lf.endsBefore); err != nil {
		return filter{}, err
	}
	if lf.minVolume < 0 || lf.minLiquidity < 0 {
		return filter{}, errors.New("--min-volume and --min-liquidity cannot be negative")
	}
	if lf.limit < 0 {
		return filter{}, errors.New("--limit cannot be negative")
	}
	return f, nil
}

// parseDate reads a day, taken as its first moment in UTC, or a full RFC 3339
// timestamp. Empty is no bound.
func parseDate(flag, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.DateOnly, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("%s %q is neither a date (2026-11-03) nor a timestamp (2026-11-03T12:00:00Z)", flag, s)
}

func (f filter) eventsQuery() api.EventsQuery {
	return api.EventsQuery{
		Order:        f.order.events,
		Ascending:    f.ascending,
		Status:       f.status,
		TagSlug:      f.tagSlug,
		VolumeMin:    f.volumeMin,
		LiquidityMin: f.liquidityMin,
		EndDateMin:   f.endDateMin,
		EndDateMax:   f.endDateMax,
	}
}

// marketsQuery leaves the tag out: the markets listing takes a tag's ID, not
// its slug, which costs a request to find.
func (f filter) marketsQuery() api.MarketsQuery {
	return api.MarketsQuery{
		Order:        f.order.markets,
		Ascending:    f.ascending,
		Status:       f.status,
		VolumeMin:    f.volumeMin,
		LiquidityMin: f.liquidityMin,
		EndDateMin:   f.endDateMin,
		EndDateMax:   f.endDateMax,
	}
}

// findTag looks up the tag the filter names, if it names one. A misspelt slug
// is an error here rather than an export of nothing: the events listing
// answers an unknown tag with an empty page.
func findTag(ctx context.Context, c *api.Client, f filter) (*api.Tag, error) {
	if f.tagSlug == "" {
		return &api.Tag{}, nil
	}
	tag, err := c.Tag(ctx, f.tagSlug)
	if api.IsNotFound(err) {
		return nil, fmt.Errorf("there is no tag with the slug %q", f.tagSlug)
	}
	return tag, err
}

// eventPages pages through the events the filter selects.
func eventPages(ctx context.Context, c *api.Client, f filter, limit int) (iter.Seq2[[]api.Event, error], error) {
	if _, err := findTag(ctx, c, f); err != nil {
		return nil, err
	}
	q := f.eventsQuery()
	q.Limit = limit
	return api.Pages(ctx, func(cursor string) ([]api.Event, string, error) {
		q.Cursor = cursor
		return c.Events(ctx, q)
	}), nil
}

// marketPages pages through the markets the filter selects, each with its
// tags.
func marketPages(ctx context.Context, c *api.Client, f filter, limit int) (iter.Seq2[[]api.Market, error], error) {
	tag, err := findTag(ctx, c, f)
	if err != nil {
		return nil, err
	}
	q := f.marketsQuery()
	q.Limit = limit
	q.IncludeTags = true
	q.TagID = tag.ID
	return api.Pages(ctx, func(cursor string) ([]api.Market, string, error) {
		q.Cursor = cursor
		return c.Markets(ctx, q)
	}), nil
}

// listDataset is one of the datasets drawn from a listing.
type listDataset struct {
	name, short string
	// open starts the listing; pageLimit is the page size to ask for.
	open func(ctx context.Context, c *api.Client, f filter, pageLimit int) (export.Dataset, error)
}

var listDatasets = []listDataset{
	{
		name:  "markets",
		short: "one row per market, with its first two outcomes",
		open: func(ctx context.Context, c *api.Client, f filter, pageLimit int) (export.Dataset, error) {
			pages, err := marketPages(ctx, c, f, pageLimit)
			if err != nil {
				return nil, err
			}
			return export.Markets(pages), nil
		},
	},
	{
		name:  "events",
		short: "one row per event",
		open: func(ctx context.Context, c *api.Client, f filter, pageLimit int) (export.Dataset, error) {
			pages, err := eventPages(ctx, c, f, pageLimit)
			if err != nil {
				return nil, err
			}
			return export.Events(pages), nil
		},
	},
	{
		name:  "outcomes",
		short: "one row per market and outcome",
		open: func(ctx context.Context, c *api.Client, f filter, pageLimit int) (export.Dataset, error) {
			pages, err := marketPages(ctx, c, f, pageLimit)
			if err != nil {
				return nil, err
			}
			return export.Outcomes(pages), nil
		},
	},
}

// newExportCommand builds `polymarket export`, with a subcommand per dataset.
func newExportCommand(o *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "export a dataset to CSV without opening the browser",
		Long: "export writes one dataset as CSV, to standard output unless -o names a file.\n\n" +
			"The file has a header row, RFC 3339 timestamps in UTC, prices as\n" +
			"decimals between 0 and 1, and an empty cell wherever Polymarket has\n" +
			"no value. A file is written under a temporary name and renamed once\n" +
			"complete, so an interrupted export leaves nothing behind.",
		Example: "polymarket export markets --tag politics --min-volume 10000 --limit 5000 -o politics.csv\n" +
			"polymarket export events --order endDate | grid",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	for _, d := range listDatasets {
		cmd.AddCommand(newListCommand(o, d))
	}
	return cmd
}

func newListCommand(o *options, d listDataset) *cobra.Command {
	var lf listFlags
	cmd := &cobra.Command{
		Use:   d.name,
		Short: d.short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd, o, d, &lf)
		},
	}
	bindListFlags(cmd, &lf)
	return cmd
}

// bindListFlags gives cmd the flags the list datasets share, parsed into lf.
func bindListFlags(cmd *cobra.Command, lf *listFlags) {
	f := cmd.Flags()
	f.StringVar(&lf.tag, "tag", "", "only what is filed under the tag with this slug, such as politics")
	f.BoolVar(&lf.closed, "closed", false, "closed ones instead of open ones")
	f.BoolVar(&lf.all, "all", false, "open and closed ones alike")
	f.StringVar(&lf.order, "order", "", "sort by this field, smallest first (default: volume24hr, largest first)")
	f.BoolVar(&lf.desc, "desc", false, "with --order, sort largest first")
	f.Float64Var(&lf.minVolume, "min-volume", 0, "only with at least this much volume")
	f.Float64Var(&lf.minLiquidity, "min-liquidity", 0, "only with at least this much liquidity")
	f.StringVar(&lf.endsAfter, "ends-after", "", "only ending on or after this date")
	f.StringVar(&lf.endsBefore, "ends-before", "", "only ending on or before this date")
	f.IntVar(&lf.limit, "limit", 0, "write at most this many rows (default: all of them)")
	f.StringVarP(&lf.output, "output", "o", "-", "write to this file; - is standard output")
	f.BoolVar(&lf.raw, "raw", false, "do not guard text starting with = + - @ against spreadsheets")
	cmd.MarkFlagsMutuallyExclusive("closed", "all")

	choices := make([]string, len(sortOrders))
	for i, s := range sortOrders {
		choices[i] = cobra.CompletionWithDesc(s.name, s.desc)
	}
	registerFlagCompletion(cmd, "order", choices)
}

func runList(cmd *cobra.Command, o *options, d listDataset, lf *listFlags) error {
	f, err := lf.filter()
	if err != nil {
		return err
	}

	// One more than the limit is enough to tell whether anything was cut off.
	pageLimit := pageSize
	if lf.limit > 0 {
		pageLimit = min(pageSize, lf.limit+1)
	}
	// fang.Execute passes the context main supplies down to here, so an
	// interrupt stops the paging.
	ctx := cmd.Context()
	ds, err := d.open(ctx, o.client(), f, pageLimit)
	if err != nil {
		return err
	}

	opts := export.Options{Limit: lf.limit, Raw: lf.raw}
	if lf.output == "-" {
		// Nothing but the data: stderr is usually the same terminal that
		// whatever reads the pipe is drawing on.
		if _, err := export.Run(ctx, ds, cmd.OutOrStdout(), opts); err != nil {
			return interrupted(ctx, err, "the output is incomplete")
		}
		return nil
	}

	stderr := cmd.ErrOrStderr()
	var p *progress
	if file, ok := stderr.(*os.File); ok && isTerminal(file) {
		p = &progress{w: stderr}
		opts.Progress = p.update
	}
	sum, err := export.File(ctx, ds, lf.output, opts)
	p.clear()
	if err != nil {
		return interrupted(ctx, err, lf.output+" was not written")
	}
	reportSummary(stderr, sum, lf.output, lf.limit)
	return nil
}

// interrupted swaps the error of an export the user stopped, which is a
// cancelled request with its whole URL, for a plain statement of the outcome.
func interrupted(ctx context.Context, err error, outcome string) error {
	if ctx.Err() == nil {
		return err
	}
	return errors.New("interrupted: " + outcome)
}

// reportSummary says what was written, and what was not. Like the progress
// below it, it is a courtesy: failing to write it is not worth an error.
func reportSummary(w io.Writer, sum export.Summary, path string, limit int) {
	rows := strconv.Itoa(sum.Rows) + " rows"
	if sum.Rows == 1 {
		rows = "1 row"
	}
	_, _ = fmt.Fprintf(w, "wrote %s of %s to %s\n", rows, sum.Dataset, path)
	if sum.Capped {
		_, _ = fmt.Fprintf(w, "note: stopped at the --limit of %d; more rows match\n", limit)
	}
	for _, note := range sum.Notes {
		_, _ = fmt.Fprintln(w, "note:", note)
	}
}

// progress keeps a running row count on one line of a terminal.
type progress struct {
	w    io.Writer
	last time.Time
}

// progressInterval is how often the count is redrawn.
const progressInterval = 100 * time.Millisecond

func (p *progress) update(rows int) {
	if now := time.Now(); now.Sub(p.last) >= progressInterval {
		p.last = now
		_, _ = fmt.Fprintf(p.w, "\r%d rows", rows)
	}
}

// clear wipes the count, leaving the line for the summary or the error. It
// does nothing on a nil progress, which is what a non-terminal gets.
func (p *progress) clear() {
	if p == nil || p.last.IsZero() {
		return
	}
	_, _ = fmt.Fprint(p.w, "\r\x1b[K")
}
