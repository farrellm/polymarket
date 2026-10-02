# polymarket — design

A terminal UI for exploring Polymarket market data and exporting it to CSV.

Status: milestones 1 (scaffold) and 2 (`internal/api`) implemented; the rest is design. Endpoint shapes in §4 were checked against the live API on 2026-10-01, and again while recording the fixtures on 2026-10-02.

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
| `charm.land/bubbles/v2` | v2.2.1 | `table`, `textinput`, `viewport`, `spinner`, `progress`, `help`, `key` |
| `charm.land/lipgloss/v2` | v2.0.6 | layout, borders, colour, tabs |
| `charm.land/huh/v2` | v2.0.3 | the export dialog and filter form |
| `charm.land/log/v2` | v2.0.1 | `--debug` log file (never the screen) |
| `github.com/charmbracelet/fang` + `spf13/cobra` | v1.0.0 / v1.10.2 | CLI, help, completion, `--version` |
| `golang.org/x/time/rate` | latest | client-side rate limiter |

No Polymarket SDK: the read endpoints are plain JSON over HTTPS and a hand-written client
of a few hundred lines is easier to test and keeps trading/signing code out of the binary.
The price sparkline is hand-rolled (block characters, ~60 lines) rather than pulling in a
chart library.

## 3. Layout of the repository

Mirrors grid: thin `main.go`, everything under `internal/`.

```
main.go                     calls cli.Execute(ctx); exit 1 on error
internal/cli/               root command (TUI), `export` subcommand, flags, version var
internal/api/               HTTP client for the three services
    client.go               base URLs, http.Client, limiter, retry, decode, User-Agent
    gamma.go                events, markets, tags, search
    clob.go                 order book
    data.go                 price history, trades
    types.go                Event, Market, Outcome, Tag, Book, PricePoint, Trade
    pager.go                generic cursor iterator
internal/export/            datasets -> CSV (no UI imports)
    dataset.go              Dataset interface, column schemas
    csv.go                  writer: temp file + atomic rename, or stdout
internal/format/            money ($1.2M), price (66.5¢), deltas, relative dates
internal/ui/                Bubble Tea models
    model.go                root model: screen stack, size, status bar, routing
    tags.go                 top level: ranked tag list, sub-tags
    browse.go               Events / Markets lists over bubbles/table, scoped to a tag
    detail.go               one market: summary, book, sparkline, trades
    filter.go               huh filter form
    exportdlg.go            huh export dialog + progress
    keys.go help.go style.go spark.go
testdata/                   recorded API responses, golden CSVs
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
| `GET /events/keyset` | `limit` (max 100), `order`, `ascending`, `after_cursor`, `closed`, `tag_id`/`tag_slug`, `title_search`, `volume_min`, `liquidity_min`, `end_date_min/max`. Returns `{events, next_cursor}`; each event embeds its `markets` and `tags`. |
| `GET /markets/keyset` | Same paging; `closed`, `tag_id`, `volume_num_min`, `liquidity_num_min`, `end_date_min/max`, `include_tag`. Returns `{markets, next_cursor}`. No text-search parameter. |
| `GET /events/{id}`, `GET /markets/{id}` | refresh one item |
| `GET /tags/slug/{slug}` | resolve a tag typed by name (`--tag`, or a tag outside the ranked set) |
| `GET /tags/slug/{slug}/related-tags/tags` | `status=active`, `omit_empty=true`; ranked sub-tags of a tag (politics → Trump, Midterms, Senate Elections, …) |
| `GET /tags` | plain array of `{id,label,slug}`, `limit` ≤ 100 with `offset`. **Not used for the tag level** — see below |
| `GET /public-search?q=` | `limit_per_type`, `page`, `events_status` (`active` or `resolved`), `events_tag`; returns `{events, pagination:{hasMore,totalResults}}`, with no `events` member at all when nothing matches |
| `GET clob/book?token_id=` | `bids`, `asks` (`{price,size}` strings), `tick_size`, `min_order_size`, `last_trade_price`, `timestamp` |
| `GET data/v2/prices-history?token_id=` | `interval` = `1h`/`6h`/`1d`/`1w`/`1m`/`max`, or `start`/`end` epoch seconds (a range of at most 15 days, else 400); `bucket_seconds`; `limit` ≤ 10000, `cursor`. Returns `{data:[{timestamp,price,resolution_seconds}], pagination}` |
| `GET data/v2/trades?condition=` | `limit` ≤ 1000, `cursor`, `start`, `end`, `side`. Returns `{data:[…], pagination:{has_more,next_cursor}}` |

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

The figures are therefore "among the top 500 events", and the header says so. A tag that
does not appear there is still reachable by typing its name (`/tags/slug/{slug}`).

### Quirks the client must absorb (all observed live)

- On a market, `outcomes`, `outcomePrices` and `clobTokenIds` are **JSON arrays encoded as
  strings**. `types.go` decodes them into `[]Outcome{Label, Price, TokenID}` in a custom
  `UnmarshalJSON`.
- Numeric fields are inconsistent: a market's `volume`/`liquidity` are strings with
  `volumeNum`/`liquidityNum` as numbers, while an event's are numbers. A small
  `flexFloat` type accepts either.
- Keyset endpoints reject `offset` with 422; paging is `after_cursor` only, and the end
  is an empty or absent `next_cursor`.
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
  given. An unknown `events_status` is ignored rather than refused.
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

┌ polymarket ─ Tags ▸ Politics ─ [Events] Markets ──────────── open · sort: vol 24h ↓ ┐
│ Sub-tags: Trump · Midterms · Senate Elections · House Elections · Primaries · …      │
│ Event                                       Markets   Vol 24h    Volume   Liq  Ends  │
│▸Democratic Presidential Nominee 2028             34    $1.1M     $412M  $9.8M  2y    │
│ …                                                                                    │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ 100 loaded · more available   / search  f filter  t sub-tag  s sort  e export  h help│
└──────────────────────────────────────────────────────────────────────────────────────┘
```

1. **Tags** (top level, the start screen) — one row per tag, ranked by 24 h volume, with
   event count and liquidity (§4, "Where the tag list comes from"). The first row is
   **All**, which opens the next level with no tag filter. `/` narrows the list as you
   type; if nothing matches, `enter` looks the typed name up as a slug.
2. **Events in a tag** — `/events/keyset?tag_slug=…`. A strip under the title lists the
   tag's sub-tags (`…/related-tags/tags`); `t` picks one, which pushes another
   Events screen scoped to that sub-tag, so the breadcrumb reads
   `Tags ▸ Politics ▸ Midterms`. A `Markets` tab on the same screen switches to the flat
   market list for the tag (`/markets/keyset?tag_id=…`), skipping the event level for
   anyone who wants every market in a tag in one table.
3. **Markets in an event** — the event's markets arrive embedded in the event, so this
   level needs no request. Single-market events skip straight to detail.
4. **Market detail** — summary header (question, state, end date, volumes), then panes:
   outcomes with bid/ask/last/spread; order book depth (top N levels each side); price
   sparkline with `1d/1w/1m/max` cycling; recent trades. The description sits in a
   `viewport`. Each pane loads independently and shows its own spinner or error.

**Overlays** — help (generated from the key map, as in grid), filter form, sub-tag
picker, export dialog.

### Keys

Follows grid where the meaning carries over.

| Key | Action |
|---|---|
| `↑ ↓ pgup pgdn space d u g G` | move, as in grid |
| `enter` / `esc` | down a level / back up |
| `T` | jump back to the Tags level from anywhere |
| `tab` | switch Events / Markets within a tag |
| `/` | Tags: narrow by name. Events/Markets: search (server-side) |
| `f` | filter form: open/closed/all, min volume, min liquidity, ends before/after |
| `t` | sub-tag picker for the current tag (type to narrow) |
| `s` / `S` | cycle sort column / flip direction |
| `r` | refresh |
| `e` | export |
| `o` | open the market on polymarket.com |
| `y` | copy slug / condition ID |
| `h` `?` | help |
| `q` `ctrl+c` | quit |

### Behaviour

- **Paging**: first page of 100 on entry; the next page is requested when the cursor
  comes within one screen of the last loaded row. The status bar shows
  "N loaded · more available" or "N loaded · end".
- **Sorting and filtering are server-side** (`order`/`ascending` and the filter
  parameters), so they apply to the whole result set rather than the loaded rows.
  Changing either resets the list and its cursor.
- **Search**: inside a tag, the Events tab searches with `title_search` on
  `/events/keyset`, which keeps the tag filter and the sort. `/markets/keyset` has no text
  parameter, so the Markets tab searches via `/public-search` (`events_tag=`) and shows
  the returned events' markets flattened. `esc` clears the search and restores the list.
- **Filters persist down the stack**: open/closed and the volume/liquidity floors set at
  one level apply to the levels below it, and are shown in the title bar.
- **Errors** never tear down the screen: a failed load shows a one-line message in the
  status bar with `r` to retry, and the rows already loaded stay.

### Bubble Tea structure

- Root `Model` owns size, a screen stack (`tags`, `browse`, `detail`; `browse` is one
  type parameterised by scope — a tag, a sub-tag or an event), the active overlay and the
  status bar; it routes `tea.WindowSizeMsg` to everything and key messages to the overlay
  if one is open, else to the top screen.
- All I/O happens in `tea.Cmd`s returning typed messages (`pageMsg`, `bookMsg`,
  `historyMsg`, `tradesMsg`, `exportProgressMsg`, `errMsg`). `Update` never blocks.
- Each request carries a generation number; a response whose generation is stale (the
  user changed sort, filter or screen meanwhile) is dropped. Each screen holds a
  `context.CancelFunc` for its in-flight requests and calls it on leaving.
- Styles live in one `style.go` built from Lip Gloss adaptive colours; price changes are
  green/red with a `+`/`-` sign so colour is never the only signal. `NO_COLOR` is honoured
  by Lip Gloss.
- Minimum size 80×24; narrower terminals drop columns right-to-left (Liquidity, Volume,
  24h Δ) and never truncate the price.

## 6. Export

### Datasets

| Dataset | Source | One row per |
|---|---|---|
| `tags` | the Tags level | tag (`id, slug, label, events, volume_24h, liquidity`) |
| `markets` | browse list (Markets tab, or an event's markets), scoped to the current tag | market |
| `events` | browse list (Events tab), scoped to the current tag | event |
| `outcomes` | same as `markets`, long layout | market × outcome |
| `history` | `/v2/prices-history` for the open market | outcome × timestamp |
| `trades` | `/v2/trades` for the open market | trade |
| `book` | `/book` snapshot for the open market | outcome × side × price level |

### In the TUI

`e` opens a huh form: dataset (pre-selected from the current screen), scope, and path.

- Scope for list datasets: **loaded rows** (instant, no requests) or **all matching the
  current filters** (drains the cursor in the background).
- Default path `./polymarket-<dataset>-<YYYYMMDD-HHMMSS>.csv`; `~` is expanded; an existing
  file prompts before overwrite.
- The export runs as a command; the status bar shows a `progress`/row count and `esc`
  cancels it. The UI stays usable meanwhile. On success the status bar shows the path and
  row count.

### Headless

```
polymarket export markets --tag politics --closed=false --order volume24hr --desc \
    --min-volume 10000 --limit 5000 -o politics.csv
polymarket export history --market <slug|id> --interval 1w -o -
polymarket export trades  --market <slug|id> --since 2026-09-01 -o trades.csv
```

`-o -` writes to stdout, so `polymarket export markets | grid` works. Progress goes to
stderr and only when it is a terminal. Flags map one-to-one onto the TUI's filter state,
and both paths call the same `export.Run(ctx, dataset, w, progress)`.

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
  reports how many were affected.
- Text cells beginning with `=`, `+`, `-` or `@` are prefixed with `'` so a spreadsheet
  does not evaluate them; `--raw` turns that off. Numeric columns are never touched.
- Written to `<path>.tmp` in the destination directory and renamed on success, so a
  cancelled or failed export leaves no partial file.
- "All matching" has a safety cap (`--limit`, default 10 000 rows in the TUI, with the
  cap stated in the dialog) because the open-market set is tens of thousands of rows.

## 7. CLI

```
polymarket                       open the TUI on the Tags level
polymarket --tag politics        start inside a tag (same flags as export); esc goes up to Tags
polymarket export <dataset> …    headless export (§6)
polymarket --version | completion <shell> | man     from fang
```

Global flags: `--debug FILE` (charm log to a file), `--timeout`, and hidden
`--gamma-url`/`--clob-url`/`--data-url` overrides for tests. `version` is stamped by
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
- `internal/export`: golden CSV per dataset from the same fixtures; quoting, formula
  guard, empty values, >2 outcomes, cancel leaves no file, stdout mode.
- `internal/ui`: drive `Update` with messages directly and assert on model state and
  `View()` substrings, as grid's `ui_test.go` does; tag aggregation from a fixture page;
  drill down and back up restoring the cursor; stale-generation responses dropped;
  paging trigger; resize down to 80×24.
- `internal/cli`: flag parsing to filter state; completion for dataset and `--order`.
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
  is a constant to tune in milestone 4 against start-up time (5 requests, in parallel).
  Tags are flat on the API side; the only hierarchy is the related-tags relation, which
  is why sub-tags are a narrowing step rather than a fifth fixed level.
- **Search is weaker than the listings** (checked in milestone 2, see §4): the Markets
  tab's search cannot apply the volume, liquidity and end-date filters or the sort, so
  milestone 5 has to either apply them client-side to the returned page or say in the
  title bar that they are suspended while searching. The 15-day cap on `start`/`end`
  price-history ranges is real, so the sparkline and `export history` use `interval`.
- **To decide in review**: binary name (`polymarket` vs something shorter such as `pm`);
  whether live auto-refresh (`--refresh 30s`) belongs in v1.

