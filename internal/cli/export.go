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

// defaultOrder is what a listing is sorted by when --order is not given,
// largest first. The service's own default is by ID, oldest first, which
// makes a poor sample of anything cut short by --limit.
const defaultOrder = "volume24hr"

func findOrder(name string) (api.SortOrder, error) {
	if o, ok := api.FindOrder(name); ok {
		return o, nil
	}
	names := make([]string, len(api.SortOrders))
	for i, o := range api.SortOrders {
		names[i] = o.Name
	}
	return api.SortOrder{}, fmt.Errorf("unknown --order %q: it is one of %s", name, strings.Join(names, ", "))
}

// listFlags are the flags of the list datasets, as typed. The root command
// takes the ones that select and sort, to open the browser on.
type listFlags struct {
	tag          string
	search       string
	closed       bool
	all          bool
	order        string
	desc         bool
	minVolume    float64
	minLiquidity float64
	endsAfter    string
	endsBefore   string
	output
}

// filter is what a listing is narrowed and sorted by: the flags, checked.
type filter struct {
	tagSlug string
	search  string
	// ordered is true when --order was given, rather than defaulted.
	ordered bool
	api.Filter
}

func (lf *listFlags) filter() (filter, error) {
	f := filter{
		tagSlug: lf.tag,
		search:  strings.TrimSpace(lf.search),
		ordered: lf.order != "",
		Filter: api.Filter{
			VolumeMin:    lf.minVolume,
			LiquidityMin: lf.minLiquidity,
		},
	}
	switch {
	case lf.all:
		f.Status = api.StatusAll
	case lf.closed:
		f.Status = api.StatusClosed
	default:
		f.Status = api.StatusOpen
	}

	name := lf.order
	if name == "" {
		name = defaultOrder
	} else {
		f.Ascending = !lf.desc
	}
	var err error
	if f.Order, err = findOrder(name); err != nil {
		return filter{}, err
	}

	if f.EndDateMin, err = parseDate("--ends-after", lf.endsAfter); err != nil {
		return filter{}, err
	}
	if f.EndDateMax, err = parseDate("--ends-before", lf.endsBefore); err != nil {
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

// parseDate reads the date a flag was given; empty is no bound.
func parseDate(flag, s string) (time.Time, error) {
	t, err := api.ParseDate(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s %q is %w", flag, s, err)
	}
	return t, nil
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
	q := f.EventsQuery()
	q.Limit = limit
	q.TagSlug = f.tagSlug
	q.TitleSearch = f.search
	return api.Pages(ctx, func(cursor string) ([]api.Event, string, error) {
		q.Cursor = cursor
		return c.Events(ctx, q)
	}), nil
}

// marketPages pages through the markets the filter selects, each with its
// tags.
//
// The markets listing has no text search, so --search goes through the
// search of events instead and takes their markets. That search ranks by
// relevance and knows nothing of the floors and dates, which are applied to
// what it returns; a sort order it cannot honour is refused rather than
// dropped.
func marketPages(ctx context.Context, c *api.Client, f filter, limit int) (iter.Seq2[[]api.Market, error], error) {
	tag, err := findTag(ctx, c, f)
	if err != nil {
		return nil, err
	}
	if f.search != "" {
		if f.ordered {
			return nil, errors.New("--order does not apply to a --search of markets, which is ranked by relevance")
		}
		return api.Pages(ctx, func(cursor string) ([]api.Market, string, error) {
			return api.SearchMarkets(ctx, c, f.Filter, f.search, f.tagSlug, cursor)
		}), nil
	}
	q := f.MarketsQuery()
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
			"polymarket export events --order endDate | grid\n" +
			"polymarket export history --market will-anna-win --interval 1w | grid",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	for _, d := range listDatasets {
		cmd.AddCommand(newListCommand(o, d))
	}
	for _, d := range marketDatasets {
		cmd.AddCommand(newMarketCommand(o, d))
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
	bindFilterFlags(cmd, lf)
	cmd.Flags().StringVar(&lf.search, "search", "", "only what matches this text: an event by its title, a market by its event")
	lf.bind(cmd)
}

// bindFilterFlags gives cmd the flags that select and sort a listing, which
// the browser takes as well as the exports.
func bindFilterFlags(cmd *cobra.Command, lf *listFlags) {
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
	cmd.MarkFlagsMutuallyExclusive("closed", "all")

	choices := make([]string, len(api.SortOrders))
	for i, s := range api.SortOrders {
		choices[i] = cobra.CompletionWithDesc(s.Name, s.Desc)
	}
	registerFlagCompletion(cmd, "order", choices)
}

func runList(cmd *cobra.Command, o *options, d listDataset, lf *listFlags) error {
	f, err := lf.filter()
	if err != nil {
		return err
	}

	// fang.Execute passes the context main supplies down to here, so an
	// interrupt stops the paging.
	ctx := cmd.Context()
	ds, err := d.open(ctx, o.client(), f, pageLimit(pageSize, lf.limit))
	if err != nil {
		return err
	}
	return write(cmd, ds, lf.output)
}

// pageLimit is the page size to ask a listing for: the most it hands out,
// or, under a --limit, one row more than that, which is enough to tell
// whether anything was cut off.
func pageLimit(most, limit int) int {
	if limit > 0 {
		return min(most, limit+1)
	}
	return most
}

// output is where a dataset is to be written, and how: the flags every
// dataset shares.
type output struct {
	path  string
	limit int
	raw   bool
}

// bind gives cmd the flags that say where the rows go.
func (out *output) bind(cmd *cobra.Command) {
	f := cmd.Flags()
	f.IntVar(&out.limit, "limit", 0, "write at most this many rows (default: all of them)")
	f.StringVarP(&out.path, "output", "o", "-", "write to this file; - is standard output")
	f.BoolVar(&out.raw, "raw", false, "do not guard text starting with = + - @ against spreadsheets")
}

// write runs the export, to standard output or to the file named.
func write(cmd *cobra.Command, ds export.Dataset, out output) error {
	ctx := cmd.Context()
	opts := export.Options{Limit: out.limit, Raw: out.raw}
	if out.path == "-" {
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
	sum, err := export.File(ctx, ds, out.path, opts)
	p.clear()
	if err != nil {
		return interrupted(ctx, err, out.path+" was not written")
	}
	reportSummary(stderr, sum, out.path, out.limit)
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
