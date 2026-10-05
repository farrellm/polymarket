# polymarket

*A terminal browser for Polymarket, with a CSV and Parquet export.*

`polymarket` reads the public market data of [Polymarket](https://polymarket.com)
and shows it in the terminal. It drills down from tags to events to markets to
one market in detail — prices, price history, order book, latest trades — and
writes whatever it shows, or everything that matches, to CSV. The same exporter
runs without the browser as `polymarket export`.

It is read-only: no account, no credentials, no trading.

```
┌ polymarket ─ Tags ▸ Politics ─ [Events] Markets ─────────── vol 24h ↓ · open ┐
│ Sub-tags: Trump · Midterms · Senate Elections · House Elections · Governor E…│
│ Event                           Markets   Vol 24h    Volume       Liq   Ends │
│▸Brazil Presidential Election         32     $2.6M     $162M    $25.0M     2d │
│ Elon Musk # tweets September …       26     $878K     $3.0M     $1.1M    -1h │
│ Next French Presidential Elec…      128     $741K     $142M    $17.7M    6mo │
│ Republican Presidential Nomin…      128     $686K     $709M    $52.5M     2y │
│ Oklahoma enacts data center m…        4     $328K     $473K     $3.0M     2y │
│ Balance of Power: 2026 Midter…        5     $313K    $15.5M     $6.5M    1mo │
│ Presidential Election Winner …      128     $286K     $712M    $59.9M     2y │
│ Will the US confirm that alie…        7     $283K    $71.3M     $1.2M    3mo │
│ Prime Minister of Israel afte…       28     $277K    $42.0M     $3.8M    25d │
│ Nobel Peace Prize Winner 2026        71     $260K    $25.6M     $1.9M    6mo │
│ US announces end of Iranian b…       23     $229K    $34.7M     $657K    3mo │
│ Israel accuses Iran/proxies o…        1     $212K     $261K    $34.8K    29d │
│ Will the U.S. invade Iran bef…        1     $211K    $70.9M     $922K    3mo │
│ Democratic Presidential Nomin…      128     $191K     $1.3B    $78.2M     2y │
│ Which party will win the Hous…        9     $177K    $12.9M     $2.6M    1mo │
│ Elon Musk # tweets September …       26     $177K     $625K     $426K     3d │
│ Brazil Presidential Election …       32     $176K     $1.9M     $1.5M     2d │
│ US-Iran ceasefire continues t…        9     $169K     $5.5M     $301K    29d │
├──────────────────────────────────────────────────────────────────────────────┤
│ 100 loaded · more available  / search  f filter  t sub-tag  s sort  e export │
└──────────────────────────────────────────────────────────────────────────────┘
```

```
┌ polymarket ─ Tags ▸ Politics ▸ Will Luiz Inácio Lula da Si… ─ [Market] About ┐
│ Will Luiz Inácio Lula da Silva win the 2026 Brazilian presidential election? │
│ open · ends 2026-10-05 (2d) · vol 24h $783K · volume $13.8M · liq $730K      │
│                                                                              │
│ Outcome                        Price     Bid     Ask  Spread    Last   24h Δ │
│▸Yes                            42.5¢   42.0¢   43.0¢    1.0¢   43.0¢   +5.0¢ │
│ No                             57.5¢   57.0¢   58.0¢    1.0¢   57.0¢   -5.0¢ │
│                                                                              │
│ Price of Yes · 1w  42.5¢  -1.0¢  low 36.5¢ · high 45.5¢                      │
│                                                                              │
│ ▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▂▂▂▂▂▂▂▂▂▂▂▂▂▂▂▂▂▂▁▁▁▁▂▂▂▂▂▂▂▂▃▃▃▃▃ │
│ ████████████████████████████████████████████████████████████████████████████ │
│                                                                              │
│ Book · Yes · 13:42:24             Trades                                     │
│   Size     Bid     Ask    Size    Time         Side  Outcome   Price  Shares │
│   3.0K   42.0¢   43.0¢   47.3K    13:40:57     BUY   Yes       43.0¢      12 │
│   9.7K   41.0¢   44.0¢   39.5K    13:36:54     BUY   Yes       43.0¢       9 │
│  45.2K   40.0¢   45.0¢    120K    13:35:54     BUY   No        58.0¢    5.6K │
│  38.1K   39.0¢   46.0¢   42.4K    13:33:36     BUY   Yes       43.0¢    2.1K │
│  75.8K   38.0¢   47.0¢   51.7K    13:31:23     BUY   Yes       43.0¢      23 │
│  84.3K   37.0¢   48.0¢   47.7K    13:31:18     BUY   Yes       43.0¢      23 │
├──────────────────────────────────────────────────────────────────────────────┤
│      tab about  i interval  r refresh  o website  e export  h help  esc back │
└──────────────────────────────────────────────────────────────────────────────┘
```

It is built on [Bubble Tea](https://github.com/charmbracelet/bubbletea) and
ships as one static binary.

## Install

```
go install github.com/farrellm/polymarket@latest
```

Or from a checkout, which stamps the version from git:

```
make install
```

`polymarket completion bash|zsh|fish` prints a completion script for the shell.

## Use

```
polymarket                         # start at the tags
polymarket --tag politics          # start inside a tag; esc leads up to the tags
polymarket --tag crypto --min-volume 10000 --order endDate
```

There are four levels, and a breadcrumb in the title bar says where you are:

1. **Tags**, ranked by the 24-hour volume of the 500 busiest open events. `/`
   narrows the list as you type; a tag that is not among them can still be
   opened by typing its name and pressing `enter`.
2. **Events** filed under a tag, a page of a hundred at a time. `tab` switches
   to the tag's **markets** in one flat list. `t` narrows to a sub-tag.
3. **Markets** of one event.
4. **One market**: its outcomes with bid, ask and last trade, a price chart,
   the order book and the latest trades. `tab` shows what the market is about.

Sorting, filtering and searching are done by the service, so they apply to
everything that matches and not only to the rows loaded.

### Keys

| | |
| --- | --- |
| `↑` `↓` `pgup` `pgdn` `space` `u` `d` `g` `G` | Move |
| `enter` / `esc` | Down a level / back up |
| `T` | Back to the tags from anywhere |
| `tab` | Events / markets of a tag; market / about on a market |
| `/` | Tags: narrow by name. Events and markets: search |
| `f` | Filter: open or closed, least volume and liquidity, end date |
| `t` | Pick a sub-tag |
| `s` / `S` | Next sort order / reverse it |
| `i` | On a market: the chart's interval (`1d`, `1w`, `1m`, `max`) |
| `r` | Refresh |
| `e` | Export |
| `o` | On a market: open it on polymarket.com |
| `y` / `Y` | On a market: copy its slug / its condition ID |
| `h` `?` | Help |
| `q` `ctrl+c` | Quit |

## Export

### From the browser

`e` asks what to write and where:

```
 Export

 ▸Dataset        [markets]  outcomes
  Rows           [the 100 loaded]  all that match
  At most        10000
  File           ./polymarket-markets-20261002-134156.csv
```

`tab` moves between the fields and `←` `→` change a choice. The datasets
offered are those of the screen: the tags; a tag's events, or its markets
(also an outcome to a row); a market's price history, trades and order book.

*Rows* is either what is on screen, which is written at once, or all there is:
every event or market that matches the filter and the search, every outcome of
a market, its trades back to the first. That is fetched in the background
while the browser stays usable; the status bar counts the rows, and `esc`
stops it. *At most* caps the number of rows, since everything that matches may
be tens of thousands; leave it blank for no limit.

A file that exists is only overwritten after a second `enter`. An export that
is stopped or fails leaves no file behind.

### From the command line

```
polymarket export markets --tag politics --min-volume 10000 --limit 5000 -o politics.csv
polymarket export events --order endDate | grid
polymarket export history --market <slug|id> --interval 1w
polymarket export trades  --market <slug|id> --since 2026-09-01 -o trades.csv
polymarket export book    --market <slug|id> --outcome yes
polymarket export outcomes --tag politics -o outcomes.parquet
```

The output is standard output unless `-o` names a file, so an export pipes
straight into [grid](https://github.com/farrellm/grid), DuckDB or anything else
that reads CSV. A file named `*.parquet` is written as Parquet instead.

| Dataset | One row per | Selected by |
| --- | --- | --- |
| `events` | event | the list flags |
| `markets` | market, with its first two outcomes | the list flags |
| `outcomes` | market and outcome | the list flags |
| `history` | outcome and moment | `--market`, `--outcome`, `--interval` |
| `trades` | trade | `--market`, `--since`, `--until` |
| `book` | outcome, side and price level | `--market`, `--outcome` |

The list flags are the ones the browser starts with: `--tag`, `--search`,
`--closed` or `--all`, `--order` with `--desc`, `--min-volume`,
`--min-liquidity`, `--ends-after` and `--ends-before`; the exports add
`--exclude-tag` and `--exclude-title` to leave events out, and `markets` and
`outcomes` take `--event ID` for the markets of given events. Every dataset
takes `--limit`, `-o`, `--raw` and `--extend`. `polymarket export <dataset>
--help` has the details. The `tags` dataset is the browser's alone.

### Extending a file

`--extend` merges an export into the file `-o` names, made earlier by the
same dataset, instead of replacing it: rows already there are updated, new
ones added, and none lost. With no file there it simply writes one.

```
polymarket export trades --market <slug|id> --extend -o trades.csv
polymarket export markets --tag senate-midterms --all --extend -o markets.parquet
```

`trades` then fetches only the trades since the newest one in the file. A
history extended with `--interval 1w` at least weekly keeps the five-minute
points the service only holds for a week.

### The midterms datasets

```
scripts/senate-dump.sh            # the first dump, into data/senate-midterms
scripts/senate-extend.sh          # bring it up to date
scripts/house-dump.sh             # the same for the House, into data/house-midterms
scripts/house-extend.sh
```

The events of the 2026 US Senate races, control of the Senate and its
leaders (less primaries, state legislatures and the French Senate), and of
the House races, control of the House and its Speaker (less primaries and
state legislatures), with their markets and outcomes, and every market's
price history, trades and order book. The runs extend a Parquet file per
market under `store/`, which the DuckDB CLI puts together into one file per
dataset; it must be installed (`pacman -S duckdb`).
`scripts/{senate,house}-parquet.sh` rebuild those without fetching anything.
`POLYMARKET` names the binary, `DUCKDB` the DuckDB CLI, and `JOBS` how many
markets are fetched at once (2, which keeps within Polymarket's rate limits).
Two runs on one directory wait for each other.

`systemd/` has a user timer that runs both extensions daily at 02:00, one
after the other, with the binary built from the checkout first. The units are
symlinked rather than copied, so a `systemctl --user daemon-reload` after
editing them is the whole deploy:

```
ln -s "$PWD"/systemd/midterms-extend.{service,timer} ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now midterms-extend.timer
journalctl --user -u midterms-extend    # how the last runs went
```

Both dumps have to have been made first. A run
missed while the machine was off happens at the next boot; the user manager
must linger (`loginctl enable-linger`) for it to run while you are logged out.

### The files

- RFC 4180 CSV in UTF-8, always with a header row. The columns of a dataset
  are fixed: new ones are only ever added at the end.
- Timestamps are RFC 3339 in UTC, prices are decimals between 0 and 1, and a
  value Polymarket did not send is an empty cell rather than a zero.
- IDs and token IDs are text: a token ID has 77 digits.
- Text starting with `=`, `+`, `-` or `@` is prefixed with `'` so that a
  spreadsheet does not evaluate it; `--raw` leaves it alone.
- A Parquet file (`-o x.parquet`) holds the same columns, typed: text, doubles,
  64-bit integers for indexes and counts, booleans, and timestamps in UTC. A
  missing value is a null, and text is never guarded.

## Development

```
make            # list the targets
make check      # what CI runs: gofmt, vet, golangci-lint, race tests
make smoke      # one request per endpoint against the live API
```

`DESIGN.md` describes how it is put together, and what the Polymarket API was
found to do.
