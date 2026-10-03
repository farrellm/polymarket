# 2026 US Senate midterms: Polymarket data

<!-- Copied here from scripts/senate-data.md in the polymarket repository on every
run of senate-dump.sh / senate-extend.sh: edit it there, not here. -->

Everything Polymarket offers on the 2026 US Senate midterms: the events, their markets
and outcomes, and each market's price history, trades and order book. It was dumped
first on 2026-10-02 by `scripts/senate-dump.sh` in `~/workspace/polymarket`. A
systemd user timer (`senate-extend.timer`) runs `scripts/senate-extend.sh` daily at
02:00 to bring it up to date. `DESIGN.md` §6 in that repository covers how it is made.

The data is **read-only output**: do not edit the CSVs by hand. The next run merges into
them by key and would keep a hand edit or overwrite it unpredictably.

## What is selected

Events tagged `senate-midterms` (the state races: winners, margins, turnout, county
winners) or `senate-elections` (control of the chamber, its leaders, close-race and
cross-race markets, and other Senate events such as confirmation votes). They are open
or closed and end on or after 2026-01-01, **less**:

- primaries (tags `primaries`/`senate-primary`, or "primary" in the title);
- state legislatures ("State Senate" in the title);
- the French Senate ("French Senate").

The markets are **all the markets of those events**, chosen by their event and not by
their own tags. On 2026-10-02 there were 167 events (18 closed) and 1,700 markets (152
closed). 1,691 of the markets are Yes/No; the rest are "Closer Senate Race: X or Y?",
whose outcomes are state names.

## Files

| File | One row per | Key (unique) | Order |
|---|---|---|---|
| `events.csv` | event | `id` | as first fetched, new events at the end |
| `markets.csv` | market, with its first two outcomes | `id` | as first fetched, new at the end |
| `outcomes.csv` | market × outcome | `market_id, outcome_index` | as first fetched |
| `history.csv` | outcome × moment × bucket width | `token_id, timestamp, resolution_seconds` | by market, outcome, time |
| `trades.csv` | fill | the whole row | newest first within each market |
| `book.csv` | snapshot × outcome × side × price level | `token_id, timestamp, side, level` | snapshots oldest first |
| `by-market/<dataset>/<market id>.csv` | the same, one market a file | | |
| `errors.log` | export that failed on the last run | | |

`history.csv`, `trades.csv` and `book.csv` are concatenated from `by-market/` at the
end of every run, in market-ID order (as text). The `by-market/` files are what is
extended; the combined files are rebuilt. Read either, not both.

Joins: `markets.event_id = events.id`; `outcomes.market_id`, `history.market_id` and
`book.market_id` = `markets.id`; `trades.condition_id = markets.condition_id`;
`token_id` names one outcome everywhere (`markets.token_id_1/2`, `outcomes.token_id`).
`trades` has no `market_id`.

## Reading the values

- **Prices** are probabilities from 0 to 1, not cents. A Yes/No market's two prices sum
  to about 1. `price_1`/`price_2` in `markets.csv` are the outcomes' prices as Polymarket
  shows them at fetch time; `best_bid`, `best_ask`, `last_trade_price`, `spread` and `change_*` are those of
  the **first** outcome only.
- **Empty cell = Polymarket sent no value**, never zero. A market that never traded has
  no volume; 283 markets have no `end_date` of their own (their event has one).
- **Closed** markets keep the last quotes they had, which are stale. Use `closed` before
  trusting `best_bid`/`best_ask`.
- **Timestamps** are RFC 3339 in UTC (`2026-11-04T00:00:00Z`).
- **IDs are text.** `token_id` is a 77-digit number: read it as a string, or it loses
  precision. `condition_id` is hex.
- `tags` is `|`-joined tag slugs. `url` is the page on polymarket.com.
- Text starting with `= + - @` would carry a leading `'` (a spreadsheet guard). None did
  on 2026-10-02.
- The figures in `events.csv`/`markets.csv` (volume, liquidity, prices) are **as of the
  last run**: each run overwrites them. They are not a time series; `history.csv` is.

### history.csv

`resolution_seconds` is the width of the bucket a point stands for, and the file mixes
widths. **Filter on it**:

- `43200`: one point every 12 hours over the market's whole life (`--interval max`,
  fetched once when the market is first seen);
- `300`: one point every 5 minutes, but only for the week before each run
  (`--interval 1w`). Daily runs make an unbroken 5-minute series from 2026-09-25 on.
  A gap of more than a week between runs leaves a hole;
- `0`: the price at the moment of a run, one per outcome per run.

A moment can have a point of each width.

### trades.csv

One row per fill. A transaction can make several fills, so `transaction_hash` is not
unique. `side` is the taker's (`BUY`/`SELL`) of the outcome `outcome`/`outcome_index`,
`size` is in shares, `price` is 0–1, and `size * price` is the USDC paid. A market's
trades of every outcome are mixed together. `proxy_wallet` is the trader; `name` and
`pseudonym` are their public profile, often empty. Trades run from 2025-07 and grow
sharply toward the election (40K in 2026-09).

### book.csv

Each run adds one snapshot of the book of every **open** market that is taking orders:
`timestamp` is the snapshot's. Within one, an outcome's bids come before its asks, and
`level` is 1 at the best price. `size` is in shares. A closed market has no book, and
neither does an open one not yet taking orders (e.g. "a candidate not listed above"),
so about 840 of the 1,700 markets have one.

## Querying

The files are large: on 2026-10-02 history alone was 3.6M rows and over 1 GB. Use
DuckDB or polars rather than pandas on the whole file. DuckDB guesses the column types
wrongly in two ways:

- it reads `outcome` as BOOLEAN from its Yes/No, then fails on "Georgia";
- it reads `token_id` as DOUBLE, which loses the 77-digit IDs.

So name those columns' types, once, in views:

```sql
SET TimeZone = 'UTC';  -- else date_trunc and the display use the local zone
CREATE VIEW events   AS FROM read_csv('events.csv');
CREATE VIEW markets  AS FROM read_csv('markets.csv',
    types = {'outcome_1': 'VARCHAR', 'outcome_2': 'VARCHAR', 'token_id_1': 'VARCHAR', 'token_id_2': 'VARCHAR'});
CREATE VIEW outcomes AS FROM read_csv('outcomes.csv', types = {'outcome': 'VARCHAR', 'token_id': 'VARCHAR'});
CREATE VIEW history  AS FROM read_csv('history.csv',  types = {'outcome': 'VARCHAR', 'token_id': 'VARCHAR'});
CREATE VIEW trades   AS FROM read_csv('trades.csv',   types = {'outcome': 'VARCHAR', 'token_id': 'VARCHAR'});
CREATE VIEW book     AS FROM read_csv('book.csv',     types = {'outcome': 'VARCHAR', 'token_id': 'VARCHAR'});

-- the daily close of each market of "Which party will win the Senate in 2026?",
-- from the 12-hour points: the Yes price of each of its markets
SELECT m.question, date_trunc('day', h.timestamp) AS day, last(h.price ORDER BY h.timestamp) AS yes
FROM history h JOIN markets m ON m.id = h.market_id
WHERE m.event_slug = 'which-party-will-win-the-senate-in-2026'
  AND h.resolution_seconds = 43200 AND h.outcome_index = 0
GROUP BY ALL ORDER BY m.question, day;
```

An event usually has several markets (one per candidate or party). Its markets'
outcomes are all called Yes and No, so always group by market (`market_id`,
`question`), never by `outcome` alone.

## Keeping it current

```
systemctl --user list-timers senate-extend.timer   # next run
journalctl --user -u senate-extend                 # how the last runs went
cat errors.log                                     # what the last run could not fetch
```

A run takes 35–45 minutes at `JOBS=2`, the most that stays inside Polymarket's rate
limits. A failed run leaves the files as they were.
