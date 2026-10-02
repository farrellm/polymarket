# polymarket — design

A terminal UI for exploring Polymarket market data and exporting it to CSV.

Status: milestones 1 (scaffold), 2 (`internal/api`), 3 (`internal/export`, `polymarket export markets|events|outcomes`), 4 (`internal/ui`: the screen stack, the breadcrumb and the Tags level), 5 (the Events and Markets lists of a tag, the markets of an event, sort, filter form, sub-tag picker, search, help; `--search` on the exports and the filter flags on the root command) and 6 (the market detail: outcomes, price chart, order book, trades and the About tab, with `o` and `y`; `polymarket export history|trades|book`) and 7 (the export dialog behind `e`, with its progress and `esc` to stop it; the README) implemented: nothing is left as design only. Endpoint shapes in §4 were checked against the live API on 2026-10-01, and again while recording the fixtures and probing the sort orders, the event cursor, the tag lookup, the search, the market lookup, the book, the price history, the trades and the cost of `include_tag` on 2026-10-02.

## 1. Summary

`polymarket` is a read-only terminal browser for Polymarket. It drills down
**Tags → Events → Markets → Market detail**, filters and searches at each level, shows one
market in detail (prices, order book, price history,
recent trades), and exports whatever is on screen, or everything matching the current
filters, to CSV. The same exporter is reachable without the TUI as `polymarket export`.

Goals: fast to open, no credentials, keyboard-driven, CSVs that load cleanly into
pandas/R/DuckDB/grid.

Non-goals: trading, wallets, authenticated endpoints, websockets/live streaming, user
portfolio views, a local database.

## 2. Stack

Same baseline as grid (`go 1.26.6` in go.mod; fang + cobra entry point; Bubble Tea v2).

| Module | Version | Used for |
|---|---|---|
| `charm.land/bubbletea/v2` | v2.0.10 | program loop, messages, commands |
| `charm.land/bubbles/v2` | v2.2.1 | `textinput`, `viewport`, `key` |
| `charm.land/lipgloss/v2` | v2.0.6 | layout, borders, colour, tabs |
| `charm.land/log/v2` | v2.0.1 | `--debug` log file (never the screen) |
| `github.com/charmbracelet/fang` + `spf13/cobra` | v1.0.0 / v1.10.2 | CLI, help, completion, `--version` |
| `golang.org/x/time/rate` | latest | client-side rate limiter |

No Polymarket SDK: the read endpoints are plain JSON over HTTPS and a hand-written client
of a few hundred lines is easier to test and keeps trading/signing code out of the binary.
The price sparkline is hand-rolled (block characters, `ui/spark.go`, ~90 lines) rather
than pulling in a chart library. It is several lines tall, an eighth of a cell to a step,
scaled from the lowest price of the interval to the highest.

The panes of the market detail say "loading…" in words where a `spinner` was the plan,
for the reason the filter form gives below: a spinner is a timer that redraws the screen
for as long as it is up. The help is built from the key map by hand as well, in two
columns, rather than with `bubbles/help`.

The lists are hand-rolled too (`ui/list.go`, ~190 lines) rather than `bubbles/table`,
which was the plan. That table leaves the cursor outside its window when the cursor is
set rather than moved, which is what re-sorting or refreshing a list under the cursor
does; it cannot right-align a column of numbers; and its highlight stops at the last
column instead of the edge. The list also owns the dropping of columns in a narrow
window (§5).

The filter form is hand-rolled as well (`ui/filter.go`, a choice and four `textinput`s),
where huh was the plan. A huh `Input` wraps a `textinput` whose cursor blink it gives no
way to turn off, which is a timer redrawing the screen for as long as the form is open,
and a huh form moves between its fields with messages of its own that the program has
to route back to it. Neither fits a screen driven by `Update` alone, in the tests or
otherwise. The export dialog (`ui/exportdlg.go`) is hand-rolled for the same reasons, on
the same pattern, so huh never became a dependency. Nor did `bubbles/progress`: an export
of everything that matches does not know how many rows are to come, so there is no
fraction to draw, and the status bar counts the rows instead.

## 3. Layout of the repository

Mirrors grid: thin `main.go`, everything under `internal/`.

```
main.go                     calls cli.Execute(ctx); exit 1 on error
internal/cli/               root command (TUI), `export` subcommand, flags, version var
    root.go                 root command, global flags, the API client they describe
    export.go               `export <dataset>`: flags -> filter -> listing -> export
    market.go               the datasets about one market: history, trades, book
internal/api/               HTTP client for the three services
    client.go               base URLs, http.Client, limiter, retry, decode, User-Agent
    gamma.go                events, markets, tags, search
    clob.go                 order book
    data.go                 price history, trades
    types.go                Event, Market, Outcome, Tag, Book, PricePoint, Trade
    filter.go               Filter and SortOrders: what a listing is narrowed and sorted
                            by, for the browser and the export flags alike
    pager.go                generic cursor iterator
internal/export/            datasets -> CSV (no UI imports)
    dataset.go              Dataset interface, column schemas
    pages.go                the iterators that fetch a market's history and book as
                            they are read, for the command line and the browser alike
    csv.go                  writer: temp file + atomic rename, or stdout
internal/format/            money ($1.2M), price (66.5¢), deltas (+3.5¢), relative dates (2y)
internal/ui/                Bubble Tea models
    model.go                root model: screen stack, size, frame, breadcrumb, status bar, routing
    list.go                 the table every level is drawn as: columns, cursor, scrolling
    tags.go                 top level: the sample, aggregation, ranked tag list, find
    browse.go               Events / Markets lists of a tag, and the markets of an event
    filter.go               the filter form
    picker.go               the sub-tag picker
    help.go                 the help, built from the key map
    keys.go style.go
    detail.go               one market: outcomes, chart, book, trades; the About tab
    spark.go                the chart: a price history resampled into block characters
    open.go                 handing a page to the system's browser
    exportdlg.go            the export dialog, and the export it sets running
testdata/                   recorded API responses; golden/ holds the golden CSVs
Makefile  .golangci.yml  .github/workflows/ci.yml  .gitignore  README.md  LICENSE
```

Dependency direction: `ui -> export -> api`, `ui -> api`, `cli -> all`. `api` and `export`
never import Bubble Tea, so both are testable without a terminal.

## 4. Polymarket API

All endpoints are public and unauthenticated. Three services:

| Service | Base URL | Used for |
|---|---|---|
| Gamma | `https://gamma-api.polymarket.com` | discovery: events, markets, tags, search |
| CLOB | `https://clob.polymarket.com` | order book |
| Data API | `https://data-api.polymarket.com/v2` | price history, trades |

### Endpoints used

| Call | Notes |
|---|---|
| `GET /events/keyset` | `limit` (max 100), `order`, `ascending`, `after_cursor`, `closed`, `tag_id`/`tag_slug`, `title_search`, `volume_min`, `liquidity_min`, `end_date_min/max`. Returns `{events, next_cursor}`; each event embeds its `markets` and `tags`. The browser asks by `tag_id`, the exports by `tag_slug`. |
| `GET /markets/keyset` | Same paging; `closed`, `tag_id`, `volume_num_min`, `liquidity_num_min`, `end_date_min/max`, `include_tag`. Returns `{markets, next_cursor}`. No text-search parameter. The browser asks with `include_tag=true`, as the exports do, so that the rows it holds can be exported with their tags: a page of 100 is then 890 KB in 0.49 s against 750 KB in 0.35 s. |
| `GET /events/{id}`, `GET /markets/{id}` | refresh one item. A market fetched this way carries neither its event nor its tags |
| `GET /markets/slug/{slug}` | the market `--market` names by its slug, with a summary of its event (no tags) |
| `GET /tags/slug/{slug}` | resolve a tag typed by name (`--tag`, or a tag outside the ranked set) |
| `GET /tags/slug/{slug}/related-tags/tags` | `status=active`, `omit_empty=true`; ranked sub-tags of a tag (politics → Trump, Midterms, Senate Elections, …) |
| `GET /tags` | plain array of `{id,label,slug}`, `limit` ≤ 100 with `offset`. **Not used for the tag level** — see below |
| `GET /public-search?q=` | `limit_per_type` (silently at most 50), `page` (from 1), `events_status` (`active` or `resolved`), `events_tag` (a slug; an ID matches nothing); returns `{events, pagination:{hasMore,totalResults}}`, with no `events` member at all when nothing matches |
| `GET clob/book?token_id=` | `bids`, `asks` (`{price,size}` strings), `tick_size`, `min_order_size`, `last_trade_price`, `timestamp` |
| `GET data/v2/prices-history?token_id=` | `interval` = `1h`/`6h`/`1d`/`1w`/`1m`/`max`, or `start`/`end` epoch seconds (a range of at most 15 days, else 400); `bucket_seconds`; `limit` ≤ 10000, `cursor`. Returns `{data:[{timestamp,price,resolution_seconds}], pagination}` |
| `GET data/v2/trades?condition=` | `limit` ≤ 1000 (more is a 400), `cursor`, `start`, `end`, `side`. Returns `{data:[…], pagination:{has_more,next_cursor}}`, newest first, the trades of every outcome of the market together |

### Where the tag list comes from

`/tags` is not a usable top level: it returns thousands of tags in no meaningful order
(the first page includes "product marekt fit" and "virgins"), carries no event count or
volume, and `is_carousel=true` yields a single tag. There is no categories endpoint.

So the tag level is **derived from the events**: fetch the top 500 open events by 24 h
volume (5 requests to `/events/keyset`, each event embeds its `tags`) and aggregate per
tag: number of events, summed 24 h volume, summed liquidity. That gives a ranked list that
reflects what is actually trading, with numbers worth showing in columns. It is computed
once per session and on `r`; the fetched events are kept, so opening a tag shows its rows
immediately while the complete, server-filtered list (`tag_slug=`) loads behind them.

The five requests run **one after another**, not in parallel as first planned: the cursor
is opaque and signed, so page 2 cannot be asked for before page 1 has answered, and a
`limit` above 100 is cut to 100 without complaint. It does not matter. A page is 9–15 MB
of JSON and takes about half a second; the rows are ranked over whatever has arrived, so
the list is on screen after the first page (0.6 s measured) and settles when the fifth is
in (1.5 s). A refresh is the other way round: the rows on screen stay until the new
sample is complete, since half a sample is a worse ranking than the old one.

The figures are therefore "among the top 500 events", and the header says so, with the
number actually held. A tag that does not appear there is still reachable by typing its
name (`/tags/slug/{slug}`).

About 480 tags come out of the 500 events. They are keyed by ID, not slug: an event may
embed a tag under a slug whose case differs from the one the lookup reports
(`Global-Rates` against `global-rates`). Polymarket's own bookkeeping tags are among them
(`Hide From New`, `Recurring`, `Earn 4%`) and are listed like any other: the only flag
that looks made for hiding them, `forceHide`, is set on Sports and Politics and on none
of those.

### Quirks the client must absorb (all observed live)

- On a market, `outcomes`, `outcomePrices` and `clobTokenIds` are **JSON arrays encoded as
  strings**. `types.go` decodes them into `[]Outcome{Label, Price, TokenID}` in a custom
  `UnmarshalJSON`.
- Numeric fields are inconsistent: a market's `volume`/`liquidity` are strings with
  `volumeNum`/`liquidityNum` as numbers, while an event's are numbers. A small
  `flexFloat` type accepts either.
- Keyset endpoints reject `offset` with 422; paging is `after_cursor` only, and the end
  is an empty or absent `next_cursor`.
- With no `order`, the keyset listings come back by ID, oldest first, so every caller
  sets one. An unknown `order` is a 422 ("order fields are not valid"). Both listings
  take `volume24hr`, `volume1wk`, `volume1mo`, `endDate` and `startDate`; total volume and
  liquidity are `volume`/`liquidity` on events but must be `volumeNum`/`liquidityNum` on
  markets, where `order=volume` compares the string field as text (9999.9 before 83460060).
- `/events/keyset` answers an unknown `tag_slug` with an empty page, not an error, so a
  tag typed by hand is checked with `/tags/slug/{slug}` first. `/markets/keyset` has no
  `tag_slug` at all and needs that lookup anyway for the `tag_id`.
- `/tags/slug/{slug}` ignores case, answers an unknown slug with 404 and one with a space
  in it with 422 ("slug is invalid"), so a name typed at the find prompt is lower-cased
  and hyphenated before it is looked up.
- The keyset cursor is opaque and signed, and `limit` above 100 is silently 100: there is
  no fetching pages of one listing in parallel.
- polymarket.com serves a market at `/event/<event slug>/<market slug>`;
  `/market/<market slug>` redirects there, which is what a market known without its
  event links to.
- A market **inside an event** has no `volume*` members at all if it has never traded,
  and `outcomePrices: null` if it has not opened yet; `/markets/{id}` reports the same
  market with `"volume": "0"`. `api.Float` therefore records whether a value was sent, so
  a missing number is not shown or exported as a zero.
- Timestamps come in three forms: RFC 3339 strings (Gamma), epoch seconds as numbers
  (Data API), and epoch milliseconds in a string (the CLOB book). `api.Time` reads all three.
- `/book` lists both sides worst price first; the client re-sorts them best first.
- On the Data API the end of a listing is `has_more: false`, not a missing cursor.
- `/public-search` honours fewer filters than the listings: a status and a tag, but no
  volume, liquidity or end-date bounds, and it ranks by relevance whatever `sort` is
  given. An unknown `events_status` is ignored rather than refused. The status is the
  event's: an open event is returned with its closed markets too (about a third of the
  markets in a page for "trump"), so the markets are filtered again on arrival.
- `/markets/slug/{slug}` matches the slug in the case it is written in (a tag's lookup
  ignores case), and answers an unknown one with 404. `/markets/{id}` answers an unknown ID
  with 404 too, but one of too many digits (twelve) with 422 ("id is invalid"), as the
  slug lookup does a slug with a space. A market is named by its ID if the name is all
  digits and by its slug otherwise, and a 422 is reported as no such market.
- A market's `bestBid`, `bestAsk`, `lastTradePrice` and `oneDayPriceChange` are of its
  **first outcome**. The second outcome of two is the same book from the other side: its
  bid is one minus the first's ask (checked against both books of a live market). The
  book's own `last_trade_price` is not per outcome: both books of a market report the same
  figure, so it is not shown.
- A closed market is still sent with the bid, ask and price change of its last hours; has
  **no book** at all (`/book` is a 404, "No orderbook exists"); and has no price history
  under any `interval` but `max`, the intervals being measured back from now. Its trades
  are all there.
- `/prices-history` with an `interval` and no `limit` returns the whole interval in one
  page (the default limit is 10000, and the service spaces the points to suit: 60 s for
  `1h`, `6h` and `1d`, 300 s for `1w`, 1800 s for `1m`, 43200 s for `max`, a couple of
  thousand points at most). The last point of a market that is trading is the price now,
  with `resolution_seconds: 0`. An unknown `token_id` is an empty page, not an error; an
  unknown `interval`, or none, is a 400.
- A description may hold tabs and carriage returns, which would move the frame: they are
  taken out before it is shown.
- Token IDs are 77-digit decimals: keep them as strings everywhere, including CSV.
- Identifiers differ per service: Gamma `id`/`slug`, CLOB `token_id` (per outcome), Data
  API `condition` (= `conditionId`, per market).

### Client behaviour

- One `api.Client` with injectable base URLs (tests point them at `httptest`).
- Every method takes a `context.Context`; none is stored in a struct (`containedctx` lint).
- Limiter: 10 req/s, burst 20, shared across services. Documented limits are far higher
  (Gamma `/events` 500 and `/markets` 300 per 10 s; Data `/v2/prices-history` 200 per 10 s),
  so this is politeness, not necessity.
- Retry on 429, 5xx and a request that got no answer (connection error, timeout): up to 3
  attempts, exponential backoff with jitter, honouring `Retry-After` up to 30 s. 4xx other
  than 429 is returned as a typed `*api.Error{Status, Body, URL}` without retrying.
- 15 s timeout per request; `User-Agent: polymarket-tui/<version>`.
- `pager.go`: `Pages[T](ctx, fetch func(cursor string) ([]T, string, error)) iter.Seq2[[]T, error]`,
  used by both the TUI ("load next page") and the exporter ("drain everything").

## 5. User interface

### Screens

Four levels, each a screen on a stack, with a breadcrumb in the title bar. `enter` goes
down a level, `esc` goes back up and restores the cursor where it was.

```
Tags  ▸  Events (in a tag)  ▸  Markets (in an event)  ▸  Market detail
```

```
┌ polymarket ─ Tags ───────────────────────────── ranked over the top 500 open events ┐
│ Tag                          Events    Vol 24h   Liquidity                           │
│ All                             500     $84.2M     $310M                             │
│▸Sports                          212     $41.0M      $96M                             │
│ Politics                         88     $12.3M      $71M                             │
│ Crypto                           64      $9.8M      $22M                             │
│ …                                                                                    │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ 143 tags                          / find tag  s sort  r refresh  e export  h help    │
└──────────────────────────────────────────────────────────────────────────────────────┘

┌ polymarket ─ Tags ▸ Politics ─ [Events] Markets ────────────────── vol 24h ↓ · open ┐
│ Sub-tags: Trump · Midterms · Senate Elections · House Elections · Primaries · …      │
│ Event                                       Markets   Vol 24h    Volume   Liq  Ends  │
│▸Democratic Presidential Nominee 2028             34    $1.1M     $412M  $9.8M  2y    │
│ …                                                                                    │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ 100 loaded · more available          / search  f filter  t sub-tag  s sort  h help   │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

1. **Tags** (top level, the start screen) — one row per tag, ranked by 24 h volume, with
   event count and liquidity (§4, "Where the tag list comes from"). The first row is
   **All**, which opens the next level with no tag filter. `/` narrows the list as you
   type, by label or slug, with the cursor on the best match; `enter` opens the row under
   the cursor, or, if nothing matches, looks the typed name up as a slug. The find stays
   set after `enter` and on the way back up, until `esc` clears it. `s` cycles the sort
   (24 h volume, events, liquidity, name) and `S` reverses it; both sort the sample in
   hand, with no request, and leave the cursor on its tag.
2. **Events in a tag** — `/events/keyset?tag_id=…`. A strip under the title lists the
   tag's sub-tags (`…/related-tags/tags`); `t` picks one, which pushes another
   Events screen scoped to that sub-tag, so the breadcrumb reads
   `Tags ▸ Politics ▸ Midterms`. A `Markets` tab on the same screen switches to the flat
   market list for the tag (`/markets/keyset?tag_id=…`), skipping the event level for
   anyone who wants every market in a tag in one table. Each tab keeps its own rows and
   cursor, and the Markets tab is only fetched once it is shown. The rows the Tags
   level holds for the tag stand in until the first page arrives, provided the filter is
   the default one: the sample is of the busiest open events and of nothing else.
3. **Markets in an event** — the event's markets arrive embedded in the event, so this
   level needs no request: it sorts, filters and searches the markets in hand, under
   their short names (`groupItemTitle`), and `r` fetches the event again
   (`/events/{id}`). A market with no figure for the field sorted by goes last either
   way round. An event with a single market skips this level: `enter` on it opens the
   market's detail.
4. **Market detail** — two tabs. **Market**: the question; a line of facts (state, end
   date, volumes); the outcomes with price, bid, ask, spread, last trade and 24 h change;
   the price chart of the outcome under the cursor, `i` cycling `1d/1w/1m/max`; and, side
   by side, that outcome's order book (as many levels each side as there are lines for)
   and the market's latest trades. **About**: what identifies the market (event, dates,
   tick size, slug, condition ID, URL) and its description, wrapped, in a `viewport` the
   movement keys scroll.

   ```
   ┌ polymarket ─ … ▸ Flávio Bolsonaro ─ [Market] About ──────────────────────────┐
   │ Will Flávio Bolsonaro win the 2026 Brazilian presidential election?          │
   │ open · ends 2026-10-05 (2d) · vol 24h $729K · volume $13.4M · liq $642K      │
   │                                                                              │
   │ Outcome                        Price     Bid     Ask  Spread    Last   24h Δ │
   │▸Yes                            56.0¢   56.0¢   56.1¢    0.1¢   56.1¢   -5.8¢ │
   │ No                             44.0¢   43.9¢   44.0¢    0.1¢   43.9¢   +5.8¢ │
   │                                                                              │
   │ Price of Yes · 1w  56.2¢  0.0¢  low 54.5¢ · high 63.6¢                       │
   │                                                         ▁▂▆▆█▇▆▆▄▅▄▂         │
   │                        ▁▁▁▁    ▁▁▁▁▁▁▁▁▁▁▁▇▆▆▆▂▃▅▅▅▆████████████████▇██ ▂    │
   │ ▂▂▇▆▄▄▄▄▄▄▄▄▄▄▇▇▇▇▇▇▇▇███████▆▇████████████████████████████████████████▅█▃▁▄ │
   │                                                                              │
   │ Book · Yes · 12:30:43             Trades                                     │
   │   Size     Bid     Ask    Size    Time         Side  Outcome   Price  Shares │
   │    959   56.1¢   56.3¢    1.0K    12:29:47     SELL  No        43.9¢      70 │
   │    300   56.0¢   56.4¢   11.0K    12:28:11     BUY   Yes       56.1¢     128 │
   │ …                                                                            │
   ├──────────────────────────────────────────────────────────────────────────────┤
   │        tab about  i interval  r refresh  o website  h help  esc back  q quit │
   └──────────────────────────────────────────────────────────────────────────────┘
   ```

   The market is the one the list held, so the screen is drawn at once and asks for
   nothing about the market itself. The outcomes' quotes are the market's own (§4): the
   first outcome's as sent, the second of two mirrored from it, and none for an outcome
   beyond those or for a market that has closed, which keeps only its last trade. The
   chart, the book and the trades are a request each (`/prices-history` with an
   `interval`, `/book`, `/trades` with `limit=50`), each pane saying for itself that it is
   loading or what went wrong while the others stand. A book or a history once fetched is
   kept, so moving the cursor back to an outcome, or the interval round to one seen, asks
   for nothing. `r` fetches the market again (`/markets/{id}`, keeping the event it was
   opened under) with the trades and the book and history on show; the rest is dropped
   and fetched when next looked at. A closed market starts on `max`, having no prices
   under a shorter interval, and its book pane says that it is not trading.

   The chart resamples the history by time into one column per character: each column
   takes the last price at or before its end. It is drawn from the lowest price of the
   interval to the highest, which the title states with the price now and the change
   over the interval. The room under the outcomes is split a third to the chart (eight
   lines at most) and the rest to the two tables. Trades are dated in the terminal's time
   zone: the time for one of today, the day and the time for one of this year, else the
   date.

   `o` opens the market's page with the system's opener (`xdg-open`, `open`,
   `rundll32`), `y` copies its slug and `Y` its condition ID, through the terminal
   (OSC 52).

**Overlays** — help (generated from the key map, as in grid), filter form, sub-tag
picker, export dialog (§6). Each takes the place of the list inside the frame rather than
floating over it. The help lists the keys of the lists, or, on a market, those of its
detail in their place: both together do not fit 24 lines. The title bar's note leads
with the sort and follows it with the search and the filter; too long for the bar, it
loses its end, then goes altogether before the breadcrumb is cut. A breadcrumb too long
with the tabs after it loses its upper levels first (`… ▸ Nominee 2028 ▸ Bob`), so that
where one is, and the tabs there, stay in view.

### Keys

Follows grid where the meaning carries over.

| Key | Action |
|---|---|
| `↑ ↓ pgup pgdn space d u g G` | move, as in grid |
| `enter` / `esc` | down a level / back up |
| `T` | jump back to the Tags level from anywhere |
| `tab` | switch Events / Markets within a tag, Market / About on a market |
| `/` | Tags: narrow by name. Events/Markets: search (server-side, on `enter`) |
| `f` | filter form: open/closed/all, min volume, min liquidity, ends before/after |
| `t` | sub-tag picker for the current tag (type to narrow) |
| `s` / `S` | cycle the sort (24 h, week, month and total volume, liquidity, end, start) / flip direction |
| `i` | on a market: cycle the chart's interval (`1d`, `1w`, `1m`, `max`) |
| `r` | refresh |
| `e` | export what the screen shows, or all of it (§6) |
| `o` | on a market: open it on polymarket.com |
| `y` / `Y` | on a market: copy its slug / its condition ID |
| `h` `?` | help |
| `q` `ctrl+c` | quit |

### Behaviour

- **Paging**: first page of 100 on entry; the next page is requested when the cursor
  comes within one screen of the last loaded row. The status bar shows
  "N loaded · more available" or "N loaded · end". A page that fails leaves the cursor
  to go on from, so moving towards the end asks for it again; `r` starts over.
- **Sorting and filtering are server-side** (`order`/`ascending` and the filter
  parameters), so they apply to the whole result set rather than the loaded rows.
  Changing either resets the list and its cursor.
- **Search**: inside a tag, the Events tab searches with `title_search` on
  `/events/keyset`, which keeps the tag filter and the sort. `/markets/keyset` has no text
  parameter, so the Markets tab searches via `/public-search` (`events_tag=`) and shows
  the returned events' markets flattened. That search honours none of the filter but the
  status, so the rest is applied to each page as it arrives (`api.Filter.Keeps`): a
  page may add no rows, and the paging goes on until the screen is full or the search
  runs out. It cannot be sorted: the note reads "by relevance" and `s`/`S` say why
  instead of acting. `esc` clears the search and restores the list.
- **Filters persist down the stack**: the filter and the sort set at one level are what
  a level opened from it starts with (an event, a sub-tag), and are shown in the title
  bar. A change made further down stays there: going back up finds the level as it was
  left. The search does not go down with them: it was a search of that list.
- **Errors** never tear down the screen: a failed load shows a one-line message in the
  status bar with `r` to retry, and the rows already loaded stay.

### Bubble Tea structure

- Root `Model` owns size, a screen stack (`tags`, `browse`, `detail`; `browse` is one
  type parameterised by scope — a tag, a sub-tag or an event), the active overlay and the
  status bar; it routes key messages to the overlay if one is open, else to the top
  screen, and sizes a screen when it comes into view. In practice the help and the
  export dialog are the root's, with the export under way: it goes on while the levels
  are moved through, so it cannot be a screen's. A screen says what it has to export
  (`exports`), and that is all it knows of it. The search prompt, the filter form and
  the picker belong to the screen that opened them, which reports `typing` so that
  letters reach it. The result of a request goes to
  every screen on the stack, since the Tags level goes on loading under a lower one; each
  recognises its own by message type, owner (two levels of the same kind may be
  stacked, a sub-tag under its tag) and generation. A screen answers a message with a
  command and a `nav` (push this screen, or pop), which only the top one may use.
- All I/O happens in `tea.Cmd`s returning typed messages (`pageMsg`, `bookMsg`,
  `historyMsg`, `tradesMsg`, `marketMsg`, `exportProgressMsg`, `exportDoneMsg`), an error
  being a field of the message it would have been. `Update` never blocks.
- Each request carries a generation number; a response whose generation is stale (the
  user changed sort, filter or screen meanwhile) is dropped. Each screen holds a
  `context.CancelFunc` for its in-flight requests and calls it on leaving.
- Styles live in one `style.go` built from Lip Gloss adaptive colours; price changes are
  green/red with a `+`/`-` sign so colour is never the only signal. `NO_COLOR` is honoured
  by Lip Gloss.
- Minimum size 80×24; narrower terminals drop columns right-to-left (Ends, Liquidity,
  Volume, 24 h volume, 24h Δ) and never truncate the price. Below the minimum the frame is clipped, bottom
  and right first, and never wraps.
- Colours are the terminal's own sixteen plus bold, faint and reverse, rather than Lip
  Gloss's light/dark pairs: they follow the terminal's theme without asking it for its
  background colour.

## 6. Export

### Datasets

| Dataset | Source | One row per |
|---|---|---|
| `tags` | the Tags level; the browser's alone, since the sample it is counted over is | tag (`id, slug, label, events, volume_24h, liquidity`) |
| `markets` | browse list (Markets tab, or an event's markets), scoped to the current tag | market |
| `events` | browse list (Events tab), scoped to the current tag | event |
| `outcomes` | same as `markets`, long layout | market × outcome |
| `history` | `/v2/prices-history` for one market | outcome × timestamp |
| `trades` | `/v2/trades` for one market | trade |
| `book` | `/book` snapshot for one market | outcome × side × price level |

### In the TUI

`e` opens a form of four fields: the dataset, which rows, how many at most, and the file.

```
 Export

 ▸Dataset        [markets]  outcomes
  Rows           [the 100 loaded]  all that match
  At most        10000
  File           ./polymarket-markets-20261002-134156.csv
```

- The datasets are the screen's own (`screen.exports`), the first chosen: `tags` on the
  Tags level; `events` on the Events tab; `markets` and `outcomes` on the Markets tab and
  on an event's markets; `history`, `trades` and `book` on a market.
- Rows are either **what the screen holds**, written with no request, or **all there
  is**, fetched as it is written:

  | Screen | Held | Fetched |
  |---|---|---|
  | Tags | the tags listed, as found and sorted, without the All row | – |
  | a tag's events, markets | the rows loaded | all that match the filter and the search, in the order on screen |
  | an event's markets | the markets listed, each given its event and its event's tags | – (they are all in hand) |
  | market: history | the chart: one outcome over the interval on show | every outcome, over that interval |
  | market: trades | the latest 50 | all of them, newest first |
  | market: book | the book on show | every outcome's, fetched again |

  What is held is copied when the dialog opens, so the export reads nothing the screen
  goes on changing.
- At most caps the rows (`export.Options.Limit`) at 10 000 unless changed; blank is no
  limit. It applies to either kind of rows.
- The file defaults to `./polymarket-<dataset>-<YYYYMMDD-HHMMSS>.csv`, and follows the
  dataset until it is edited; a leading `~/` is the home directory. The dialog checks it
  on `enter`: a missing directory or a directory in the file's place keeps the dialog
  open, and a file that exists is overwritten only on a second `enter`.
- The export runs on a goroutine of its own, started by a command, and reports through a
  channel that a command listens on: a row count at most every 100 ms, and the end. The
  status bar reads `exporting markets: 1401 rows` with `esc stop export` first among the
  hints; the browser stays usable meanwhile, except that `esc` stops the export before it
  means anything else, and a second export has to wait. A prompt keeps the status bar
  and `esc` while it is open.
- The status bar then says how it ended, until the next key: `wrote 250 rows to <file>`
  (`wrote the first 250 rows …` if the cap cut it short, then the dataset's notes), that it
  was cancelled and the file not written, or what went wrong. The file is named and the
  dataset is not, since at 80 columns there is room for one and the default file name
  holds the other.
- Quitting stops an export and waits for it to have stopped, so that its temporary file
  is removed.

### Headless

```
polymarket export markets --tag politics --closed=false --order volume24hr --desc \
    --min-volume 10000 --limit 5000 -o politics.csv
polymarket export history --market <slug|id> --interval 1w -o -
polymarket export trades  --market <slug|id> --since 2026-09-01 -o trades.csv
```

Each dataset is a subcommand of `export`, so it has its own flags and completes like any
other command. The list datasets (`markets`, `events`, `outcomes`) share one set:

| Flag | Meaning |
|---|---|
| `--tag SLUG` | only what is filed under the tag; an unknown slug is an error |
| `--search TEXT` | events whose title contains the text; for `markets` and `outcomes`, the markets of the events a search finds |
| `--closed`, `--all` | closed ones, or both, instead of open ones (mutually exclusive) |
| `--order FIELD`, `--desc` | `volume24hr`, `volume1wk`, `volume1mo`, `volume`, `liquidity`, `endDate`, `startDate`; ascending unless `--desc`. Without `--order` the sort is `volume24hr`, largest first |
| `--min-volume`, `--min-liquidity` | floors |
| `--ends-after`, `--ends-before` | a date (`2026-11-03`, midnight UTC) or an RFC 3339 timestamp |
| `--limit N` | at most N rows; the default headless is all of them |
| `-o FILE` | the file to write; the default, `-`, is stdout |
| `--raw` | no formula guard |

Stdout is the default output, so `polymarket export markets | grid` works. Writing there
prints nothing else at all: stderr is usually the terminal the reader of the pipe is
drawing on. Writing a file reports a summary line on stderr (rows written, whether the
limit cut it short, the dataset's notes), preceded by a running row count when stderr is a
terminal. An interrupt cancels the context, so the temporary file is removed.

Flags map one-to-one onto the TUI's filter state, and both paths call the same
`export.Run(ctx, dataset, w, options)`, or `export.File` for the temp-file-and-rename
around it. `Options` carries the row limit, `Raw` and the progress callback; the result is
a `Summary{Rows, Capped, Notes}`. A `Dataset` is built from an iterator of pages
(`export.Markets(pages)`), which is `api.Pages(…)` for "all matching" and
`export.Loaded(rows)` for the rows on screen. The datasets about a market are built the
same way: `export.Trades` from pages of trades, `export.History` from pages of
`Series{Market, Outcome, Points}` and `export.Book` from `Depth{Market, Outcome, Book}`,
so that the browser can hand over what it holds, and either it or the command line an
iterator that fetches as it goes (`export.HistoryPages`, `export.BookPages`).

The datasets about one market (`history`, `trades`, `book`) take none of those but the
last three, and name their market instead:

| Flag | Meaning |
|---|---|
| `--market REF` | the market, by its slug or, if all digits, its ID (required); an unknown one is an error |
| `--outcome NAME` | `history`, `book`: only this outcome, by its label (`yes`, any case) or its index from 0; the default is every outcome, one after another |
| `--interval I` | `history`: `1h`, `6h`, `1d`, `1w`, `1m` or `max`; the default is `max` |
| `--since`, `--until` | `trades`: a date or a timestamp, as `--ends-after` takes |

`history` pages each outcome to its end (10000 points a page) before starting the next;
`trades` pages newest first (1000 a page); `book` is one request per outcome, and an
error if the market has no book, which is to say is not trading, rather than a file of
headers. A market that has not opened has no tokens to ask about, and says so.

`--search` does what `/` does in the browser. On `events` it is one more parameter of the
listing, and everything else still applies. On `markets` and `outcomes` it goes through
`/public-search` (`api.SearchMarkets`, paged by page number through the same
`api.Pages`): the status and the tag go to the service, the floors and dates are applied
to what comes back, and the order is the search's own, so `--order` with it is refused
rather than quietly dropped.

### CSV format

- `encoding/csv`, RFC 4180, UTF-8, `\n` line endings, header row always present.
- Stable `snake_case` columns per dataset, defined once in `dataset.go` and covered by
  golden tests, so a column is never renamed or reordered by accident.
- Timestamps RFC 3339 in UTC. Prices as decimals in 0–1 (not cents). Numbers written with
  the shortest round-tripping representation; values the API sends as strings are passed
  through untouched. IDs and token IDs always as text. Missing values are empty cells.
- `markets` columns: `id, slug, question, event_id, event_slug, event_title, condition_id,
  active, closed, accepting_orders, neg_risk, start_date, end_date, outcome_1, price_1,
  token_id_1, outcome_2, price_2, token_id_2, best_bid, best_ask, last_trade_price,
  spread, change_1h, change_1d, change_1w, volume, volume_24h, volume_1w, volume_1m,
  liquidity, tags, url`. `tags` is `|`-joined. A market with more than two outcomes is
  exported in full only by `outcomes`; `markets` writes the first two and the summary line
  reports how many were affected. `tags` holds slugs, which cannot contain the separator,
  and is filled in because the listing is asked with `include_tag=true`.
- `events` columns: `id, slug, title, active, closed, neg_risk, start_date, end_date,
  markets, volume, volume_24h, volume_1w, volume_1m, liquidity, open_interest,
  comment_count, tags, url`. `markets` is the number of markets in the event.
- `outcomes` columns: `market_id, market_slug, question, event_id, event_slug,
  event_title, condition_id, active, closed, end_date, outcome_index, outcome, price,
  token_id`. `outcome_index` counts from 0, as the Data API's trades do.
- `history` columns: `market_id, market_slug, question, condition_id, outcome_index,
  outcome, token_id, timestamp, price, resolution_seconds`. Each outcome's rows are
  oldest first. `resolution_seconds` is the width of the bucket the price stands for, and
  0 on the last row of an outcome still trading: its price now.
- `trades` columns: `timestamp, condition_id, market_slug, event_slug, question, side,
  outcome_index, outcome, token_id, price, size, proxy_wallet, name, pseudonym,
  transaction_hash`. `side` is the taker's (`BUY`, `SELL`) and `size` is in shares. Every
  cell is the trade's own, so the dataset needs no market beside it.
- `book` columns: `market_id, market_slug, question, condition_id, outcome_index, outcome,
  token_id, timestamp, side, level, price, size`. `side` is `bid` or `ask`, an outcome's
  bids before its asks; `level` counts from 1 at the best price of a side; `timestamp` is
  the snapshot's.
- Every column has a kind (text, number, bool, time). The client parses numbers on the
  way in (`api.Float`), so they are written back in their shortest round-tripping form
  rather than byte for byte.
- Text cells beginning with `=`, `+`, `-` or `@` are prefixed with `'` so a spreadsheet
  does not evaluate them; `--raw` turns that off. Only columns of the text kind are
  touched, so a negative number stays a number.
- Written to `<path>.tmp` in the destination directory and renamed on success, so a
  cancelled or failed export leaves no partial file.
- `tags` columns: `id, slug, label, events, volume_24h, liquidity`. The figures are sums
  over the sample of events the Tags level ranks by, so an event counts towards each of
  its tags.
- "All matching" has a safety cap (`--limit`, default 10 000 rows in the TUI, where it is
  a field of the dialog) because the open-market set is tens of thousands of rows.

## 7. CLI

```
polymarket                       open the TUI on the Tags level
polymarket --tag politics        start inside a tag; esc goes up to Tags
polymarket export <dataset> …    headless export (§6)
polymarket --version | completion <shell> | man     from fang
```

The root command takes the export's flags that select and sort (`--tag`, `--closed`,
`--all`, `--order`, `--desc`, `--min-volume`, `--min-liquidity`, `--ends-after`,
`--ends-before`): they are the filter every list starts with, and with `--tag` the
browser opens inside that tag, which is looked up first so that a misspelt one is an
error rather than an empty screen. `--search` is not among them.

Without a terminal on both stdin and stdout the root command is an error that points at
`polymarket export`, rather than a frame drawn into a pipe.

Global flags: `--debug FILE` (charm log to a file; not there yet),
`--timeout`, and hidden `--gamma-url`/`--clob-url`/`--data-url` overrides for tests. `version` is stamped by
`-ldflags` into `internal/cli.version`, exactly as grid does. No config file in v1.

## 8. Tooling (taken from `../grid`)

**Makefile** — grid's, with `BINARY := polymarket`, `MODULE := github.com/farrellm/polymarket`,
the same `VERSION`/`LDFLAGS`/`GOBIN` logic and the self-documenting `help` default target.

| Target | Does |
|---|---|
| `help` | list targets (default) |
| `build` / `install` / `uninstall` | as grid |
| `run` | build, then start the TUI |
| `test` / `race` / `cover` | as grid |
| `fmt` | `gofmt -w .` |
| `vet` | `go vet ./...` |
| `lint` | `golangci-lint run`, falling back to `go run …@$(GOLANGCI_VERSION)`; pinned `v2.13.2` |
| `tidy` | `go mod tidy` |
| `check` | everything CI runs: gofmt check, vet, lint, race tests |
| `fixtures` | re-record `testdata/*.json` from the live API with `testdata/record.go` (new; replaces grid's generator). Tests on the fixtures assert shape, not values, so a new recording does not break them |
| `smoke` | `go test -tags live ./internal/api/...` against the real API (new; not in `check`) |
| `clean` | remove the binary and `coverage.out` |

Dropped from grid: `bench`, `benchstat`, `bench-old`, `bench-new` (nothing here is
performance-critical enough to benchmark yet).

**`.golangci.yml`** — grid's file unchanged: `version: "2"`; linters `errcheck, govet,
ineffassign, staticcheck, unused, revive, gocritic, misspell, copyloopvar, perfsprint,
bodyclose, nilerr, unconvert, containedctx`; the same explicit revive rule list;
`misspell.locale: UK`; errcheck excluded in `_test.go`; formatter `gofmt`;
`max-issues-per-linter: 0`, `max-same-issues: 0`. `bodyclose` and `containedctx` matter
more here than in grid, since this is an HTTP client.

**CI** (`.github/workflows/ci.yml`) — grid's `test` job (ubuntu + macOS: gofmt check, vet,
build, `go test -race`) and `lint` job (`golangci-lint-action@v8`, version kept in step
with the Makefile). No fixtures or bench jobs. CI never touches the live API.

**`.gitignore`** — grid's, with `/polymarket` as the build output and `*.csv` at the root
so stray exports are not committed (`!testdata/**/*.csv`).

## 9. Testing

- `internal/api`: `httptest.Server` serving recorded responses from `testdata/`; cases for
  the string-encoded arrays, string-or-number floats, cursor paging to exhaustion, 429 with
  `Retry-After`, 5xx retry, context cancellation, and the 422 error path.
- `internal/export`: golden CSV per dataset (`testdata/golden/`, rewritten with
  `go test ./internal/export -update`) from JSON written into the test, since the
  recorded fixtures change with every recording and are only checked for shape; quoting,
  formula guard, empty values, >2 outcomes, cancel leaves no file, stdout mode.
- `internal/ui`: drive `Update` with messages directly and assert on model state and
  `View()` substrings, as grid's `ui_test.go` does, over a `Client` that answers at once
  so a test can run every command a key sets off (`settle`) or one round at a time
  (`step`); tag aggregation from a fixture page; drill down and back up restoring the
  cursor; stale-generation responses dropped; paging trigger; resize down to 80×24 and
  below; the market detail over recorded books, histories and trades: one request per
  pane, the cache per outcome and interval, each pane failing alone, a refresh overtaken;
  the export dialog on every level, writing into a temporary directory the test has moved
  into: what each dataset holds, the requests "all there is" makes, the cap, the checks
  on the file, an export stopped by `esc` and by quitting leaving nothing behind.
- `internal/cli`: flag parsing to filter state; completion for dataset, `--order` and
  `--interval`; the export commands end to end against an `httptest` stand-in for the
  three services.
- `make smoke` (build tag `live`): one request per endpoint, asserting only shape, to catch
  API drift. Run by hand, not in CI.

## 10. Milestones

1. Scaffold: `go.mod`, `main.go`, `cli` root, Makefile, `.golangci.yml`, CI, `.gitignore`; `make check` green on an empty app.
2. `internal/api` with fixtures and tests; `make smoke`.
3. `internal/export` + `polymarket export markets|events|outcomes`.
4. Screen stack and breadcrumb; Tags level (aggregation, ranking, find-as-you-type).
5. Events / Markets lists scoped to a tag: tabs, paging, sort, status bar, help; markets
   in an event. Then the filter form, sub-tag picker and search.
6. Market detail: outcomes, book, sparkline, trades; `export history|trades|book`.
7. Export dialog with progress and cancel; README with a screenshot.

## 11. Risks and open questions

- **API drift**: Gamma's keyset endpoints and Data API v2 are recent; field names differ
  between v1 and v2 (`proxyWallet` vs `proxy_wallet`). Mitigation: tolerant decoding
  (unknown fields ignored, absent fields zero), the `smoke` target, base-URL overrides.
- **Geo-restrictions**: read endpoints answered from here without restriction; if a
  region is blocked the app reports the HTTP status and body rather than retrying.
- **Tag ranking is a sample**: counts and volumes on the Tags level cover the top 500
  open events, not all of them, and a niche tag may be missing until typed by name. 500
  stays: the five requests cannot run in parallel (§4), but they take 1.5 s between them
  and the list is usable after the first.
  Tags are flat on the API side; the only hierarchy is the related-tags relation, which
  is why sub-tags are a narrowing step rather than a fifth fixed level.
- **Search is weaker than the listings** (checked in milestone 2, see §4): the Markets
  tab's search cannot apply the volume, liquidity and end-date filters or the sort.
  Settled in milestone 5: the filters are applied client-side to each page, and the
  sort is suspended and said to be (§5, Search). The 15-day cap on `start`/`end`
  price-history ranges is real, so the sparkline and `export history` use `interval`.
- **To decide in review**: binary name (`polymarket` vs something shorter such as `pm`);
  whether live auto-refresh (`--refresh 30s`) belongs in v1.

