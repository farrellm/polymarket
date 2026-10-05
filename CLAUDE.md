# polymarket

Read-only terminal browser for Polymarket, in Go. `DESIGN.md` is the spec: work is done
per milestone (§10). Update its status line and record anything the live API contradicts.
A milestone is two commits: the work with its `DESIGN.md` changes, then `Record milestone N learnings in CLAUDE.md`.

## Commands

- `make check` - what CI runs: gofmt check, vet, golangci-lint, race tests. Must pass before a commit.
- `make smoke` - one request per endpoint against the live API (build tag `live`); not in `check` or CI. Its subtests need the `events` one to have run, so `-run TestLive/search` alone fails.
- `make fixtures` - re-records `testdata/*.json` from the live API via `testdata/record.go`. It re-records all of them: to add one, run a copy of `record.go` cut down to the new call, from the repository root.
- `golangci-lint run --build-tags live` and `go vet -tags live ./...` - `make check` does not cover `smoke_test.go`.
- After `go get`, run `go mod tidy` before building: a new bubbles sub-package pulls in modules `go get` leaves out of `go.sum`.

## Conventions

- `../grid` is the sibling project the tooling and code style come from; copy its patterns.
- Comments and identifiers use UK spelling (`misspell` locale UK): honour, normalise, colour.
- `perfsprint` rejects `fmt.Sprint` on integers; use `strconv`.
- `perfsprint` also rejects `s += …` in a loop, tests included; collect into a slice and `strings.Join`.
- No `context.Context` in structs (`containedctx`), test tables included; every API call takes one as a parameter.
- `errcheck` is on outside `_test.go`: write `_, _ = fmt.Fprintf(w, …)` for courtesy output such as progress.
- `nilerr` rejects `return nil` on a path where an error is known to be non-nil (`if ctx.Err() != nil { return nil }`); return the error.
- `api` and `export` must never import Bubble Tea.
- `ui` screens draw with `list` (`internal/ui/list.go`), not `bubbles/table`: see `DESIGN.md` §2 for why.
- A `ui` request keeps its context in the closure of its `tea.Cmd` and its `CancelFunc` on the screen; a message carries a generation number, and a stale one is dropped.
- Results of requests go to every screen on the stack, keys only to the top one: a screen must ignore messages that are not its own.
- A request's message also carries its owner (`owner *browse`): two screens of one kind can be stacked, a sub-tag under its tag.
- The filter form is hand-rolled (`internal/ui/filter.go`), not huh: a huh `Input` cannot have its cursor blink turned off.
- An overlay (prompt, form, picker) belongs to the screen that opened it; the help and the export dialog are the root's: a new one needs a branch in the screen's `typing`, `hints`, `status`, `view` and both halves of `update` (keys, and other messages for a paste).
- A pane of the market detail (`internal/ui/detail.go`) says "loading…" in words: a `spinner` is a tick that re-arms itself, which hangs `settle`.
- A request of the detail goes through `detail.request`, which keeps its `CancelFunc`; books are kept per token and histories per token and interval, and `r` drops all but those on show.
- A table inside a pane is `list.plain()` over fixed-width columns: a column of leftover width under 16 makes the list drop columns.
- Text of the service's shown outside a list goes through `plainText`: a tab in a description moves the frame.
- The help has two sets of groups (`helpColumns(market)`), since both do not fit 24 lines: a key of the detail goes in its Market group.
- The hints in the status bar are the bindings' help, dropped from the right when short of room: keep a binding's help to a word or two.
- The export dialog and the export under way are the root `Model`'s (`internal/ui/exportdlg.go`): a screen only says what it offers in `exports()`, the dataset most its own first.
- An `exportScope` copies what it takes from a screen (`slices.Clone`): the export reads it on its own goroutine while the screen goes on changing its rows.
- A running export reports through `exportJob.updates`, which a command listens on and which closes at the end: that command ends, so it does not hang `settle` as a tick would.
- What the root says in the status bar (`Model.notice`, the export's row count) gives way to a prompt of the top screen; keep it short enough for 80 columns, the default file name alone being 40.
- A page is opened through `env.openURL` (`Options.OpenURL`), never `openInBrowser` directly: `newModel` records it on `fakeClient.opened`.
- New prompts come from `newPrompt` (`internal/ui/tags.go`), which turns the blink off.
- The filter and sort shared by `ui` and `cli` are `api.Filter` and `api.SortOrders` (`internal/api/filter.go`); a new level copies its parent's filter.
- A `textinput` gets its cursor blink turned off (`Styles().Cursor.Blink = false`), and non-key messages are forwarded to it while it is open, or a paste never arrives.
- The root command decides whether it has a terminal from `cmd.InOrStdin()`/`cmd.OutOrStdout()`, never `os.Stdout`: `go test` on one package inherits the real terminal.
- The root command shares `bindFilterFlags` with the exports and checks those flags before it looks for a terminal, so a CLI test can reject them; the `--tag` lookup comes after.
- The exports about one market (`history`, `trades`, `book`) are `marketDatasets` in `internal/cli/market.go`; every export writes through `write` with the shared `output` flags.
- The iterators that fetch a market's history and book (`export.HistoryPages`, `export.BookPages`) are shared by `cli` and `ui`: change them there, not in either.
- `polymarket export` with `-o -` (the default) writes only data: nothing goes to stderr, since a pipe reader such as grid is drawing on that terminal.
- Export columns are a contract: never rename or reorder one; add at the end and update the goldens.
- A path ending in `.parquet` is written as Parquet (`isParquet`); stdout and the TUI stay CSV. Only `internal/export/parquet.go` imports arrow, and a column's Parquet type comes from its `Kind`: an index or count is `Integer`, not `Number`.
- Parquet goes through the same rows of cells as CSV, unguarded, and is read back as the cells a CSV would hold, so `merge` and `Newest` serve both. `pqarrow.FileWriter.Close` closes a sink that is an `io.Closer`: hand it a bare `io.Writer`, since `replace` closes the file.
- `--extend` merges by a key per dataset in `merges` (`internal/export/extend.go`): a new extendable dataset needs an entry there, and `TestMergesNameColumnsOfTheirDatasets` checks its columns exist.
- Flags of the output (`--limit`, `--extend`) are checked in `output.check`, which `filter()` and `query()` call before any request.
- `--event`, `--exclude-tag` and `--exclude-title` are the list exports' only (`bindListFlags`), not the root command's filter flags; an exclusion tests a market by its event's title and by its own tags with its event's.
- A market's trades go through `export.TradePages`, never `api.Pages` over `Client.Trades`: it applies the bounds the service ignores.
- The midterms datasets are `scripts/{senate,house}-*.sh` into `data/{senate,house}-midterms/` (ignored): thin scripts that set `CHAMBER` and source `scripts/midterms-common.sh`, where the chambers differ only in `select_events`. Each copies its `scripts/<chamber>-data.md` in as `CLAUDE.md`: change the description there. `shellcheck -x scripts/*.sh` must be clean.
- The exports extend Parquet files under `data/<chamber>-midterms/store/`. Readers get the six `*.parquet` files that `scripts/midterms-parquet.sql` (DuckDB CLI) combines and sorts from them; the types come from the exports.
- A run holds `DIR/.lock` (`flock`): a second run on the same dataset waits. A dump that dies is finished by `<chamber>-extend.sh`, since a market without a history file gets the whole of it.
- No tag holds the House's control as `senate-elections` does the Senate's: the House takes `midterms` events with `--search House` beside `house-elections`.
- `systemd/midterms-extend.{service,timer}` are symlinked into `~/.config/systemd/user/`: after editing, `systemctl --user daemon-reload`. The service runs the Senate (~40 min) and then the House (~3½ h, 8,858 markets), never both at once: two runs at JOBS=2 would break the price history's rate limit.
- A long run started from a Claude Code background shell can be killed when memory runs short; start it with `systemd-run --user --unit=<name>` and follow it with `journalctl --user -u <name>`.

## Testing

- Fixture tests assert shape, not values: a re-recording holds different events.
  Put exact-value cases in inline JSON instead.
- `go test ./internal/export -update` rewrites the golden CSVs in `testdata/golden/`; read the diff before committing it.
- CLI tests run `newCommand(&options{})` directly (not fang) with the hidden `--gamma-url`, `--clob-url` and `--data-url` pointed at `httptest` under `/gamma`, `/clob` and `/data`; see `service` in `internal/cli/export_test.go`.
- `newTestClient` in `internal/api/api_test.go` points a client at `httptest` with the
  limiter off and the retry pauses recorded rather than slept.
- UI tests drive `Model.Update` over `fakeClient` (`internal/ui/ui_test.go`): `press` runs a key and
  every command it sets off, `step` runs one round, and assertions read the stripped `View()`.
- `settle` in the UI tests runs commands until none is left, so a command that re-arms itself (a tick, a blink) hangs the test.
- `fakeClient.Events` narrows its pages by `TagID` and `TitleSearch` as the service would; the other listings are handed out as they are.
- The CLI `service`'s trades are all at 1790947065 (2026-10-02T13:17:45Z): a `--since`/`--until` test must bracket that, or the rows are dropped.
- To check the real data, query its Parquet files with `duckdb` (installed system-wide). To read a CSV export, use DuckDB, not `awk -F,`, since questions hold commas. Name `outcome` and `token_id` VARCHAR, or it guesses BOOLEAN and DOUBLE.
- Extend tests run over `formats` (`.csv`, `.parquet`), and `readBack` reads either. Under `set -o pipefail`, `| head` exits 141.
- `fakeClient` answers a token not in `books` with a 404, which is a market not trading; `history` is keyed `"<token> <interval>"`.
- `market()` names its slug, condition ID and tokens after its ID (`slug-m2`, `0xm2`, `m2-yes`) and quotes it a cent either side of its price.
- `bob(t)` (`internal/ui/detail_test.go`) opens the detail of `nominee`'s busiest market; pick a pane's line with `find(t, m, text)`, since the panes move with the height.
- `event()` gives an event two zero-value markets, which pass an open filter and render as rows of `–`; use `market()` and `nominee()` (`browse_test.go`) where the markets are looked at.
- Under a real tag the list starts a line lower, below the strip of sub-tags: the header is `lines(m)[2]`, or use `headerLine(m)`; `rowLabels` allows for it.
- `politics(t)` (`internal/ui/browse_test.go`) opens a browser inside Politics, 120 columns wide: at 80 the title bar's note is cut short, so assert a whole note only at 120.
- An export test moves into a directory of its own with `workDir(t)` (`internal/ui/exportdlg_test.go`) and reads the file back with `wantCells`; the default file name ends in `stamp`.
- To catch an export under way, take the command from `m.Update(keyMsg("enter"))` without running it: the export starts when the command does, so `esc` or `q` before `settle` is a deterministic cancel.
- `retype(m, text)` replaces the text of the field in focus (ctrl+u, then the letters).
- The help of the lists is exactly the 20 lines an 80×24 window has: another binding there needs a group moved to the left column.
- Tests stop the clock with `newModel` (`Options.Now` = `testNow`); a screen reads the time from `env.now`, never `time.Now`.
- A `Model` is 80×24 until it gets a `tea.WindowSizeMsg`; rows that tie sort by name as text (`Tag 10` before `Tag 2`).
- To look at the real thing: `tmux new-session -d -s pm -x 80 -y 24 ./polymarket`, then `tmux send-keys`
  and `tmux capture-pane -p`. Send `Escape` on its own: followed at once by a key it reads as alt+key.

## API gotchas

- Probe the live API with `curl` before trusting an endpoint shape; §4 of `DESIGN.md` lists the quirks found so far.
- A market inside an event may have no volume or prices at all: check `api.Float.Valid`, never assume zero means zero.
- Always set `order` on the keyset listings: the default is by ID, oldest first.
- Keyset pages cannot be fetched in parallel (the cursor is opaque) and `limit` above 100 is silently 100.
- Key tags by ID: the slug embedded in an event can differ in case from the one `/tags/slug/` reports.
- A page of `/events/keyset` is 9–15 MB: `curl -s -o` it to a file and query it with `jq`; time it with `curl -w '%{time_total}'` (there is no `/usr/bin/time`).
- `/tags/slug/{slug}` ignores case, 404s on an unknown slug and 422s on one with a space.
- `/events/keyset` answers an unknown `tag_slug` with an empty page, not an error; check the tag with `Client.Tag` first (`findTag`).
- Sort fields differ per listing: markets need `volumeNum`/`liquidityNum` (`order=volume` sorts as text). Go through `api.SortOrders`.
- `/public-search`: `limit_per_type` above 50 is silently 50, `events_tag` takes a slug (an ID matches nothing), and an open event comes with its closed markets. Go through `api.SearchMarkets`.
- A market's `bestBid`, `bestAsk`, `lastTradePrice` and price changes are of its first outcome; the second of two is one minus them (`quoteOf`). A closed market still carries stale ones.
- `/book` is a 404 for a market that is not trading, and its `last_trade_price` is the same on both outcomes' books: do not show it.
- `/prices-history` intervals count back from now, so a closed market has points only under `max`; the last point of a live one is the price now, with `resolution_seconds: 0`; an unknown token is an empty page.
- `/markets/slug/{slug}` is case-sensitive and brings the event; `/markets/{id}` brings neither event nor tags, and 422s on an ID of twelve digits.
- `include_tag=true` adds about a fifth to a page of `/markets/keyset` (890 KB against 750 KB); the browser asks with it so that loaded markets export with their tags.
- The Data API refuses a `limit` over its maximum with a 400 (trades 1000, history 10000) where Gamma silently caps it.
- `/v2/trades` accepts `start`/`end` and ignores them (v1 `/trades?market=` honours `start`).
- Rate limits are per 10 s: prices-history 200, Gamma `/markets` 300, `/events` 500, book 1500; over them requests are delayed, not refused. The client's 10 req/s is per process, so parallel CLI runs multiply it.
- A market's own tags differ from its event's, and one in `/markets/keyset` comes with an event that has none: select by event (`--event`), not by the markets listing's tag, when the events decide.
- An open market may have no book (not taking orders yet, e.g. "a candidate not listed above"); 283 of the Senate markets embedded in events have no `endDate` of their own.
