# 2026 US House midterms: Polymarket data

<!-- Copied here from scripts/house-data.md in the polymarket repository on every
run of house-dump.sh / house-extend.sh / house-parquet.sh: edit it there, not here. -->

Everything Polymarket offers on the 2026 US House midterms: the events, their markets
and outcomes, and each market's price history, trades and order book. It was dumped
first on 2026-10-04 by `scripts/house-dump.sh` in `~/workspace/polymarket`. A
systemd user timer (`midterms-extend.timer`) runs `scripts/house-extend.sh` daily at
02:00 to bring it up to date. `DESIGN.md` §6 in that repository covers how it is made.

The data is **read-only output**: do not edit the files by hand. The next run merges into
the files under `store/` by key, and would keep a hand edit or overwrite it
unpredictably. It then rebuilds the Parquet files from them, which overwrites any edit to one.

## What is selected

Events tagged `house-elections` (the district races: each district's winner, and its
margin of victory), and events tagged `midterms` whose title matches "House" (control
of the chamber, seats by state, the Speaker, the popular vote, turnout, and markets on
the House odds). No tag holds the control of the House as `senate-elections` does the
Senate's, hence the search. They are open or closed and end on or after 2026-01-01,
**less**:

- primaries (tags `primaries`/`house-primary`/`house-primaries`, or "primary" in the
  title);
- state legislatures ("State House" in the title).

The markets are **all the markets of those events**, chosen by their event and not by
their own tags. On 2026-10-04 there were 909 events (7 closed) and 8,858 markets (56
closed). All but two of the markets are Yes/No: "Which party will win the House in
2026?" has a market of Democratic Party/Republican Party, and one of the House-odds
markets is Up/Down.

## Files

Read the Parquet files. The columns are typed: IDs are text, prices and sizes are
doubles, indexes and counts are integers, flags are booleans, and times are
timestamps in UTC.

| File | One row per | Key (unique) | Order |
|---|---|---|---|
| `events.parquet` | event | `id` | as first fetched, new events at the end |
| `markets.parquet` | market, with its first two outcomes | `id` | as first fetched, new at the end |
| `outcomes.parquet` | market × outcome | `market_id, outcome_index` | as first fetched |
| `history.parquet` | outcome × moment × bucket width | `token_id, timestamp, resolution_seconds` | `market_id` (as text), `outcome_index`, `timestamp`, `resolution_seconds` |
| `trades.parquet` | fill | the whole row | `condition_id`, `timestamp` (oldest first) |
| `book.parquet` | snapshot × outcome × side × price level | `token_id, timestamp, side, level` | `market_id` (as text), `timestamp`, `outcome_index`, bids before asks, `level` |
| `errors.log` | export that failed on the last run | | |
| `store/` | the working store the runs extend | | |

`store/` holds `events.parquet`, `markets.parquet` and `outcomes.parquet`, and
`by-market/<dataset>/<market id>.parquet` for history, trades and book, with the same
columns and types as the files above. The exports merge into these files. At the end of
every run, `scripts/midterms-parquet.sql` puts them together and sorts them into the six
files above. The two hold the same rows; read the six, which are sorted and whole.
`scripts/house-parquet.sh` rebuilds them without fetching anything.

Joins: `markets.event_id = events.id`; `outcomes.market_id`, `history.market_id` and
`book.market_id` = `markets.id`; `trades.condition_id = markets.condition_id`;
`token_id` names one outcome everywhere (`markets.token_id_1/2`, `outcomes.token_id`).
`trades` has no `market_id`.

## Reading the values

- **Prices** are probabilities from 0 to 1, not cents. A Yes/No market's two prices sum
  to about 1. `price_1`/`price_2` in `markets` are the outcomes' prices as Polymarket
  shows them at fetch time; `best_bid`, `best_ask`, `last_trade_price`, `spread` and `change_*` are those of
  the **first** outcome only.
- **NULL = Polymarket sent no value**, never zero. A market
  that never traded has no volume.
- **Closed** markets keep the last quotes they had, which are stale. Use `closed` before
  trusting `best_bid`/`best_ask`.
- **Timestamps** are `TIMESTAMP WITH TIME ZONE`, stored in UTC.
- **IDs are text.** This includes `token_id`, a 77-digit number that any numeric type
  would lose precision on. `condition_id` is hex. Market IDs sort as text.
- `tags` is `|`-joined tag slugs. `url` is the page on polymarket.com.
- Text is as Polymarket sent it: no spreadsheet guard.
- The figures in `events`/`markets` (volume, liquidity, prices) are **as of the
  last run**: each run overwrites them. They are not a time series; `history` is.

### history

`resolution_seconds` is the width of the bucket a point stands for, and the file mixes
widths. **Filter on it**:

- `43200`: one point every 12 hours over the market's whole life (`--interval max`,
  fetched once when the market is first seen);
- `300`: one point every 5 minutes, but only for the week before each run
  (`--interval 1w`). Daily runs make an unbroken 5-minute series from 2026-09-25 on.
  A gap of more than a week between runs leaves a hole;
- `0`: the price at the moment of a run, one per outcome per run.

A moment can have a point of each width.

### trades

One row per fill. A transaction can make several fills, so `transaction_hash` is not
unique. `side` is the taker's (`BUY`/`SELL`) of the outcome `outcome`/`outcome_index`,
`size` is in shares, `price` is 0–1, and `size * price` is the USDC paid. A market's
trades of every outcome are mixed together. `proxy_wallet` is the trader; `name` and
`pseudonym` are their public profile, often NULL. Trades grow sharply toward the
election.

### book

Each run adds one snapshot of the book of every **open** market that is taking orders:
`timestamp` is the snapshot's. Within one, an outcome's bids come before its asks, and
`level` is 1 at the best price. `size` is in shares. A closed market has no book, and
neither does an open one not yet taking orders (e.g. "a candidate not listed above"):
744 of the 8,802 open markets were not on 2026-10-04.

## Querying

Use DuckDB or polars. History is most of the rows, nearly all of them 5-minute points.
The types are in the files, so a view needs no more than the file:

```sql
SET TimeZone = 'UTC';  -- else date_trunc and the display use the local zone
CREATE VIEW events   AS FROM 'events.parquet';
CREATE VIEW markets  AS FROM 'markets.parquet';
CREATE VIEW outcomes AS FROM 'outcomes.parquet';
CREATE VIEW history  AS FROM 'history.parquet';
CREATE VIEW trades   AS FROM 'trades.parquet';
CREATE VIEW book     AS FROM 'book.parquet';

-- the daily close of each market of "Which party will win the House in 2026?",
-- from the 12-hour points: the Yes price of each of its markets
SELECT m.question, date_trunc('day', h.timestamp) AS day, last(h.price ORDER BY h.timestamp) AS yes
FROM history h JOIN markets m ON m.id = h.market_id
WHERE m.event_slug = 'which-party-will-win-the-house-in-2026'
  AND h.resolution_seconds = 43200 AND h.outcome_index = 0
GROUP BY ALL ORDER BY m.question, day;
```

An event usually has several markets (one per candidate or party, or one per bracket
of a margin of victory). A district is in the event's title (`CA-22 House Election
Winner`), not a column of its own. Its markets'
outcomes are all called Yes and No, so always group by market (`market_id`,
`question`), never by `outcome` alone.

## Keeping it current

```
systemctl --user list-timers midterms-extend.timer   # next run
journalctl --user -u midterms-extend                 # how the last runs went
cat errors.log                                     # what the last run could not fetch
```

A run takes about 3½ hours at `JOBS=2`, the most that stays inside Polymarket's rate
limits. The timer runs the Senate's extension first, so the House's starts around 02:40. A failed run leaves the Parquet files as they were. The store keeps whatever the run
merged into it before it failed, and the next run's build picks that up.
