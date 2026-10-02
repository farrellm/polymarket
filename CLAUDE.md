# polymarket

Read-only terminal browser for Polymarket, in Go. `DESIGN.md` is the spec: work is done
per milestone (§10). Update its status line and record anything the live API contradicts.

## Commands

- `make check` - what CI runs: gofmt check, vet, golangci-lint, race tests. Must pass before a commit.
- `make smoke` - one request per endpoint against the live API (build tag `live`); not in `check` or CI. Its subtests need the `events` one to have run, so `-run TestLive/search` alone fails.
- `make fixtures` - re-records `testdata/*.json` from the live API via `testdata/record.go`.
- `golangci-lint run --build-tags live` and `go vet -tags live ./...` - `make check` does not cover `smoke_test.go`.
- After `go get`, run `go mod tidy` before building: a new bubbles sub-package pulls in modules `go get` leaves out of `go.sum`.

## Conventions

- `../grid` is the sibling project the tooling and code style come from; copy its patterns.
- Comments and identifiers use UK spelling (`misspell` locale UK): honour, normalise, colour.
- `perfsprint` rejects `fmt.Sprint` on integers; use `strconv`.
- No `context.Context` in structs (`containedctx`), test tables included; every API call takes one as a parameter.
- `errcheck` is on outside `_test.go`: write `_, _ = fmt.Fprintf(w, …)` for courtesy output such as progress.
- `nilerr` rejects `return nil` on a path where an error is known to be non-nil (`if ctx.Err() != nil { return nil }`); return the error.
- `api` and `export` must never import Bubble Tea.
- `ui` screens draw with `list` (`internal/ui/list.go`), not `bubbles/table`: see `DESIGN.md` §2 for why.
- A `ui` request keeps its context in the closure of its `tea.Cmd` and its `CancelFunc` on the screen; a message carries a generation number, and a stale one is dropped.
- Results of requests go to every screen on the stack, keys only to the top one: a screen must ignore messages that are not its own.
- A request's message also carries its owner (`owner *browse`): two screens of one kind can be stacked, a sub-tag under its tag.
- The filter form is hand-rolled (`internal/ui/filter.go`), not huh: a huh `Input` cannot have its cursor blink turned off.
- New prompts come from `newPrompt` (`internal/ui/tags.go`), which turns the blink off.
- The filter and sort shared by `ui` and `cli` are `api.Filter` and `api.SortOrders` (`internal/api/filter.go`); a new level copies its parent's filter.
- A `textinput` gets its cursor blink turned off (`Styles().Cursor.Blink = false`), and non-key messages are forwarded to it while it is open, or a paste never arrives.
- The root command decides whether it has a terminal from `cmd.InOrStdin()`/`cmd.OutOrStdout()`, never `os.Stdout`: `go test` on one package inherits the real terminal.
- `polymarket export` with `-o -` (the default) writes only data: nothing goes to stderr, since a pipe reader such as grid is drawing on that terminal.
- Export columns are a contract: never rename or reorder one; add at the end and update the goldens.

## Testing

- Fixture tests assert shape, not values: a re-recording holds different events.
  Put exact-value cases in inline JSON instead.
- `go test ./internal/export -update` rewrites the golden CSVs in `testdata/golden/`; read the diff before committing it.
- CLI tests run `newCommand(&options{})` directly (not fang) with the hidden `--gamma-url` pointed at `httptest`; see `service` in `internal/cli/export_test.go`.
- `newTestClient` in `internal/api/api_test.go` points a client at `httptest` with the
  limiter off and the retry pauses recorded rather than slept.
- UI tests drive `Model.Update` over `fakeClient` (`internal/ui/ui_test.go`): `press` runs a key and
  every command it sets off, `step` runs one round, and assertions read the stripped `View()`.
- `settle` in the UI tests runs commands until none is left, so a command that re-arms itself (a tick, a blink) hangs the test.
- `fakeClient.Events` narrows its pages by `TagID` and `TitleSearch` as the service would; the other listings are handed out as they are.
- `politics(t)` (`internal/ui/browse_test.go`) opens a browser inside Politics, 120 columns wide: at 80 the title bar's note is cut short, so assert a whole note only at 120.
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
