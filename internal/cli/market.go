package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/farrellm/polymarket/internal/api"
	"github.com/farrellm/polymarket/internal/export"
)

const (
	// historyPageSize and tradesPageSize are the most the Data API hands out
	// per request of each.
	historyPageSize = 10000
	tradesPageSize  = 1000

	// defaultInterval is how far back a history goes when --interval is not
	// given: all the way.
	defaultInterval = "max"
)

// marketFlags are the flags of the datasets about one market, as typed. No
// dataset takes all of them.
type marketFlags struct {
	market   string
	outcome  string
	interval string
	since    string
	until    string
	output
}

// marketQuery is what a dataset about one market is narrowed by: the flags,
// checked.
type marketQuery struct {
	// ref is the market's slug or ID, and outcome the label or the index of
	// the one outcome wanted, empty for all of them.
	ref      string
	outcome  string
	interval string
	since    time.Time
	until    time.Time
}

func (mf *marketFlags) query() (marketQuery, error) {
	q := marketQuery{
		ref:      strings.TrimSpace(mf.market),
		outcome:  strings.TrimSpace(mf.outcome),
		interval: mf.interval,
	}
	if q.ref == "" {
		return marketQuery{}, errors.New("--market needs the slug or the ID of a market")
	}
	if q.interval != "" && !slices.Contains(api.HistoryIntervals, q.interval) {
		return marketQuery{}, fmt.Errorf("unknown --interval %q: it is one of %s",
			q.interval, strings.Join(api.HistoryIntervals, ", "))
	}
	var err error
	if q.since, err = parseDate("--since", mf.since); err != nil {
		return marketQuery{}, err
	}
	if q.until, err = parseDate("--until", mf.until); err != nil {
		return marketQuery{}, err
	}
	if !q.since.IsZero() && !q.until.IsZero() && q.until.Before(q.since) {
		return marketQuery{}, errors.New("--until is earlier than --since")
	}
	if err := mf.check(); err != nil {
		return marketQuery{}, err
	}
	return q, nil
}

// findMarket looks up the market the flags name. One the service refuses
// to look for is as absent as one it cannot find: an ID with too many
// digits, or a slug with a space in it, is answered with a 422.
func findMarket(ctx context.Context, c *api.Client, ref string) (*api.Market, error) {
	m, err := c.FindMarket(ctx, ref)
	var apiErr *api.Error
	if api.IsNotFound(err) || errors.As(err, &apiErr) && apiErr.Status == http.StatusUnprocessableEntity {
		return nil, fmt.Errorf("there is no market with the slug or ID %q", ref)
	}
	return m, err
}

// outcomesOf picks the outcomes of a market to export: the one named, by its
// label or its index from zero, or every one. Only outcomes with a token can
// be asked about, and a market that has not opened has none.
func outcomesOf(m *api.Market, name string) ([]int, error) {
	var picked []int
	for i, o := range m.Outcomes {
		if o.TokenID == "" {
			continue
		}
		if name == "" || strings.EqualFold(name, o.Label) || name == strconv.Itoa(i) {
			picked = append(picked, i)
		}
	}
	switch {
	case len(picked) > 0:
		return picked, nil
	case name == "":
		return nil, fmt.Errorf("the market %s has no tokens: it has not opened for trading", m.Slug)
	}
	labels := make([]string, len(m.Outcomes))
	for i, o := range m.Outcomes {
		labels[i] = o.Label
	}
	return nil, fmt.Errorf("the market %s has no outcome %q: it has %s", m.Slug, name, strings.Join(labels, ", "))
}

// marketDataset is one of the datasets about a single market.
type marketDataset struct {
	name, short, example string
	// bind adds the flags that are the dataset's own.
	bind func(cmd *cobra.Command, mf *marketFlags)
	// open starts the dataset for the market found; limit is the --limit.
	open func(ctx context.Context, c *api.Client, m *api.Market, q marketQuery, limit int) (export.Dataset, error)
	// resume, if set, narrows the query of an --extend to what the file at
	// path does not hold yet.
	resume func(path string, q *marketQuery) error
}

func bindOutcomeFlag(cmd *cobra.Command, mf *marketFlags) {
	cmd.Flags().StringVar(&mf.outcome, "outcome", "",
		"only this outcome, by its label (yes) or its index from 0 (default: all of them)")
}

var marketDatasets = []marketDataset{
	{
		name:    "history",
		short:   "the price history of a market: one row per outcome and moment",
		example: "polymarket export history --market will-anna-win --interval 1w --outcome yes",
		bind: func(cmd *cobra.Command, mf *marketFlags) {
			bindOutcomeFlag(cmd, mf)
			cmd.Flags().StringVar(&mf.interval, "interval", defaultInterval,
				"how far back to go: "+strings.Join(api.HistoryIntervals, ", "))
			registerFlagCompletion(cmd, "interval", []string{
				cobra.CompletionWithDesc("1h", "the last hour, a point a minute"),
				cobra.CompletionWithDesc("6h", "the last six hours, a point a minute"),
				cobra.CompletionWithDesc("1d", "the last day, a point a minute"),
				cobra.CompletionWithDesc("1w", "the last week, a point every five minutes"),
				cobra.CompletionWithDesc("1m", "the last month, a point every half hour"),
				cobra.CompletionWithDesc("max", "everything, a point every twelve hours"),
			})
		},
		open: func(ctx context.Context, c *api.Client, m *api.Market, q marketQuery, limit int) (export.Dataset, error) {
			outcomes, err := outcomesOf(m, q.outcome)
			if err != nil {
				return nil, err
			}
			return export.History(export.HistoryPages(ctx, c, m, outcomes, q.interval, pageLimit(historyPageSize, limit))), nil
		},
	},
	{
		name:    "trades",
		short:   "the trades of a market, newest first: one row per fill",
		example: "polymarket export trades --market will-anna-win --since 2026-09-01 -o trades.csv",
		bind: func(cmd *cobra.Command, mf *marketFlags) {
			f := cmd.Flags()
			f.StringVar(&mf.since, "since", "", "only trades on or after this date")
			f.StringVar(&mf.until, "until", "", "only trades before this date")
		},
		open: func(ctx context.Context, c *api.Client, m *api.Market, q marketQuery, limit int) (export.Dataset, error) {
			if m.ConditionID == "" {
				return nil, fmt.Errorf("the market %s has no condition ID to ask for its trades by", m.Slug)
			}
			tq := api.TradesQuery{
				ConditionID: m.ConditionID,
				Limit:       pageLimit(tradesPageSize, limit),
				Start:       q.since,
				End:         q.until,
			}
			return export.Trades(export.TradePages(ctx, c, tq)), nil
		},
		// Without --since, an extension starts at the newest trade the file
		// holds: those of that second are fetched again, and merged away.
		resume: func(path string, q *marketQuery) error {
			if !q.since.IsZero() {
				return nil
			}
			var err error
			q.since, err = export.Newest(path, "timestamp")
			return err
		},
	},
	{
		name:    "book",
		short:   "the order book of a market now: one row per outcome, side and price level",
		example: "polymarket export book --market will-anna-win | grid",
		bind:    bindOutcomeFlag,
		open: func(ctx context.Context, c *api.Client, m *api.Market, q marketQuery, _ int) (export.Dataset, error) {
			outcomes, err := outcomesOf(m, q.outcome)
			if err != nil {
				return nil, err
			}
			return export.Book(export.BookPages(ctx, c, m, outcomes)), nil
		},
	},
}

func newMarketCommand(o *options, d marketDataset) *cobra.Command {
	var mf marketFlags
	cmd := &cobra.Command{
		Use:     d.name,
		Short:   d.short,
		Example: d.example,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMarket(cmd, o, d, &mf)
		},
	}
	cmd.Flags().StringVar(&mf.market, "market", "", "the market, by its slug or its ID (required)")
	d.bind(cmd, &mf)
	mf.bind(cmd)
	if err := cmd.MarkFlagRequired("market"); err != nil {
		panic(err)
	}
	return cmd
}

func runMarket(cmd *cobra.Command, o *options, d marketDataset, mf *marketFlags) error {
	q, err := mf.query()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	client := o.client()
	if mf.extend && d.resume != nil {
		if err := d.resume(mf.path, &q); err != nil {
			return err
		}
	}
	m, err := findMarket(ctx, client, q.ref)
	if err != nil {
		return err
	}
	ds, err := d.open(ctx, client, m, q, mf.limit)
	if err != nil {
		return err
	}
	return write(cmd, ds, mf.output)
}
