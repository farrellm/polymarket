# polymarket

Read-only terminal browser for Polymarket, in Go. `DESIGN.md` is the spec: work is done
per milestone (§10). Update its status line and record anything the live API contradicts.

## Commands

- `make check` - what CI runs: gofmt check, vet, golangci-lint, race tests. Must pass before a commit.
- `make smoke` - one request per endpoint against the live API (build tag `live`); not in `check` or CI.
- `make fixtures` - re-records `testdata/*.json` from the live API via `testdata/record.go`.
- `golangci-lint run --build-tags live` and `go vet -tags live ./...` - `make check` does not cover `smoke_test.go`.

## Conventions

- `../grid` is the sibling project the tooling and code style come from; copy its patterns.
- Comments and identifiers use UK spelling (`misspell` locale UK): honour, normalise, colour.
- `perfsprint` rejects `fmt.Sprint` on integers; use `strconv`.
- No `context.Context` in structs (`containedctx`); every API call takes one as a parameter.
- `api` and `export` must never import Bubble Tea.

## Testing

- Fixture tests assert shape, not values: a re-recording holds different events.
  Put exact-value cases in inline JSON instead.
- `newTestClient` in `internal/api/api_test.go` points a client at `httptest` with the
  limiter off and the retry pauses recorded rather than slept.

## API gotchas

- Probe the live API with `curl` before trusting an endpoint shape; §4 of `DESIGN.md` lists the quirks found so far.
- A market inside an event may have no volume or prices at all: check `api.Float.Valid`, never assume zero means zero.
